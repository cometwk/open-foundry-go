/**
 * Relationship (care-team) grant/revoke API — v0.2.0 Epic A1.
 *
 * Governed surface for the direct `[user]` ReBAC grants that the action pipeline
 * does NOT auto-mint. Link-derived tuples (e.g. AdmittedTo→admitted_to,
 * BedInWard→bed_in_ward) are already synced by the action executor
 * (syncLinkTuple); what has no in-platform write surface are the directly-
 * assignable user relations the FGA model declares — e.g. patient.clinician,
 * patient.nurse_in_charge, ward.assigned. Without them, can_admit/discharge/
 * transfer fail at the authorize stage and integrators must write tuples out of
 * band. This API closes that footgun.
 *
 * Hardened per review:
 * - allowlist: ONLY relations declared as directly-assignable to `user` in the
 *   merged FGA model may be granted (never computed relations like can_admit,
 *   never link-typed direct relations like admitted_to). Pack-agnostic.
 * - authorization: the caller must hold a granter role (default admin /
 *   nurse_in_charge); unauthorized callers are denied (403) and the denial is
 *   audited.
 * - audit: every grant/revoke (and every denial) emits an audit record — the
 *   underlying writeRelationship/deleteRelationship primitives do not.
 */

import type { ApiDependencies, ResolverContext } from '../graphql/types.js';
import type { RestRoute, RestRequest, RestResponse } from '../rest/types.js';
import { createRestErrorResponse } from '../rest/errors.js';
import { toSnakeCase } from '../utils.js';

/** objectType (snake_case) → set of directly-grantable `[user]` relations. */
export type GrantAllowlist = Map<string, Set<string>>;

/** Roles permitted to grant/revoke relationships. */
export const DEFAULT_GRANTER_ROLES = ['admin', 'nurse_in_charge'] as const;

// Minimal structural view of the OpenFGA model JSON (compatible with server.ts
// FgaAuthorizationModel) — avoids a circular import.
interface FgaModelLike {
  type_definitions: Array<{
    type: string;
    metadata?: {
      relations?: Record<string, { directly_related_user_types?: Array<{ type: string }> }>;
    };
  }>;
}

/**
 * Derive the grant allowlist from the merged FGA model: per type, the relations
 * whose directly_related_user_types include `user`. Excludes computed relations
 * (no metadata entry) and link-typed direct relations (e.g. admitted_to: [ward]).
 */
export function buildGrantAllowlist(model: FgaModelLike): GrantAllowlist {
  const allowlist: GrantAllowlist = new Map();
  for (const td of model.type_definitions) {
    const rels = td.metadata?.relations ?? {};
    const grantable = new Set<string>();
    for (const [rel, meta] of Object.entries(rels)) {
      if ((meta.directly_related_user_types ?? []).some((t) => t.type === 'user')) {
        grantable.add(rel);
      }
    }
    if (grantable.size > 0) allowlist.set(td.type, grantable);
  }
  return allowlist;
}

interface GrantBody {
  user?: string;
  relation?: string;
  objectType?: string;
  objectId?: string;
}

function parseBody(body: unknown): Required<GrantBody> | { error: string } {
  const b = (body ?? {}) as GrantBody;
  const missing = (['user', 'relation', 'objectType', 'objectId'] as const).filter(
    (k) => typeof b[k] !== 'string' || !(b[k] as string).trim(),
  );
  if (missing.length > 0) {
    return { error: `Missing required field(s): ${missing.join(', ')}` };
  }
  return {
    user: b.user!.trim(),
    relation: b.relation!.trim(),
    objectType: b.objectType!.trim(),
    objectId: b.objectId!.trim(),
  };
}

/** Normalise a bare id to an OpenFGA user string (`alice` → `user:alice`). */
function fgaUser(user: string): string {
  return user.includes(':') ? user : `user:${user}`;
}

function callerCanGrant(roles: string[]): boolean {
  return roles.some((r) => (DEFAULT_GRANTER_ROLES as readonly string[]).includes(r));
}

/**
 * Build the relationship grant/revoke routes. `verb` is 'link' (grant) or
 * 'unlink' (revoke) for the audit operation type.
 */
export function generateRelationshipRoutes(
  deps: ApiDependencies,
  allowlist: GrantAllowlist,
): RestRoute[] {
  const handle = async (
    action: 'grant' | 'revoke',
    req: RestRequest,
    ctx: ResolverContext,
  ): Promise<RestResponse> => {
    const { user, requestContext } = ctx;
    const traceId = requestContext.traceId;

    const parsed = parseBody(req.body);
    if ('error' in parsed) {
      return createRestErrorResponse({
        code: 'VALIDATION_ERROR', category: 'validation',
        message: parsed.error, retryable: false, traceId,
      });
    }

    const objectTypeSnake = toSnakeCase(parsed.objectType);
    const grantable = allowlist.get(objectTypeSnake);

    // Validate the relation is directly grantable (never computed/link relations).
    if (!grantable || !grantable.has(parsed.relation)) {
      return createRestErrorResponse({
        code: 'INVALID_RELATION', category: 'validation',
        message:
          `Relation '${parsed.relation}' is not a directly-grantable relation on ` +
          `'${parsed.objectType}'. Grantable: ${grantable ? [...grantable].join(', ') : '(none)'}.`,
        retryable: false, traceId,
      });
    }

    const subject = fgaUser(parsed.user);
    const resource = `${objectTypeSnake}:${parsed.objectId}`;
    const auditActor = { type: 'user' as const, id: user.id, roles: user.roles };

    // Authorization gate: only granter roles may grant/revoke. Audit denials.
    if (!callerCanGrant(user.roles)) {
      await deps.auditWriter?.write({
        actor: auditActor,
        operation: { type: action === 'grant' ? 'link' : 'unlink', objectType: objectTypeSnake, objectId: parsed.objectId },
        detail: { result: 'denied', denialReason: `Caller lacks a granter role (${DEFAULT_GRANTER_ROLES.join('/')})`, after: { subject, relation: parsed.relation } },
        traceId,
      });
      return createRestErrorResponse({
        code: 'FORBIDDEN', category: 'authorization',
        message: `Not permitted to ${action} relationships (requires one of: ${DEFAULT_GRANTER_ROLES.join(', ')}).`,
        retryable: false, traceId,
      });
    }

    try {
      if (action === 'grant') {
        await deps.authorizationService.writeRelationship(subject, parsed.relation, resource);
      } else {
        await deps.authorizationService.deleteRelationship(subject, parsed.relation, resource);
      }
      await deps.auditWriter?.write({
        actor: auditActor,
        operation: { type: action === 'grant' ? 'link' : 'unlink', objectType: objectTypeSnake, objectId: parsed.objectId },
        detail: { result: 'success', after: { subject, relation: parsed.relation } },
        traceId,
      });
      return {
        status: 200,
        body: { data: { subject, relation: parsed.relation, object: resource, [action === 'grant' ? 'granted' : 'revoked']: true } },
      };
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Relationship write failed';
      await deps.auditWriter?.write({
        actor: auditActor,
        operation: { type: action === 'grant' ? 'link' : 'unlink', objectType: objectTypeSnake, objectId: parsed.objectId },
        detail: { result: 'error', denialReason: message, after: { subject, relation: parsed.relation } },
        traceId,
      });
      return createRestErrorResponse({
        code: 'RELATIONSHIP_WRITE_FAILED', category: 'system',
        message, retryable: true, traceId,
      });
    }
  };

  return [
    { method: 'POST', pattern: '/api/v1/relationships', handler: (req, ctx) => handle('grant', req, ctx) },
    { method: 'DELETE', pattern: '/api/v1/relationships', handler: (req, ctx) => handle('revoke', req, ctx) },
  ];
}
