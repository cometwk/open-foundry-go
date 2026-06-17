import type { ObjectManager, LinkManager, ObjectSetManager } from '@openfoundry/engine';
import type { ActionExecutor, ActionManifest } from '@openfoundry/actions';
import type {
  AuthorizationService,
  OidcAuthenticator,
  ConsentService,
  AuditWriter,
} from '@openfoundry/security';
import type { ParsedSchema } from '@openfoundry/odl';
import type { RequestContext, StorageProvider, DataPurpose } from '@openfoundry/spi';

/**
 * Registry that resolves action names to parsed YAML manifests.
 * Loaded at startup from domain-pack action files.
 */
export interface ManifestRegistry {
  get(actionName: string): ActionManifest | undefined;
}

/**
 * Dependencies injected into the GraphQL API layer.
 */
export interface ApiDependencies {
  schema: ParsedSchema;
  objectManager: ObjectManager;
  linkManager: LinkManager;
  actionExecutor: ActionExecutor;
  authorizationService: AuthorizationService;
  authenticator: OidcAuthenticator;
  consentService?: ConsentService;
  auditWriter?: AuditWriter;
  storage: StorageProvider;
  manifestRegistry?: ManifestRegistry;
  objectSetManager?: ObjectSetManager;
  /**
   * Allowlist of directly-grantable `[user]` relations per object type (snake),
   * derived from the merged FGA model. Powers the relationship grant/revoke
   * API (REST + GraphQL). Absent → the grant surface rejects everything.
   */
  grantAllowlist?: Map<string, Set<string>>;
  /**
   * Platform roles permitted to grant/revoke relationships. Deployment policy
   * (env RELATIONSHIP_GRANTER_ROLES). Absent → generic default (`admin`). NHS
   * deployments add clinical roles (e.g. nurse_in_charge) via config.
   */
  granterRoles?: readonly string[];
  /**
   * Platform roles permitted to record consent decisions. Deployment policy
   * (env CONSENT_RECORDER_ROLES). Absent → generic default (`admin`).
   */
  consentRecorderRoles?: readonly string[];
}

/**
 * Resolved context available in every GraphQL resolver.
 */
export interface ResolverContext {
  requestContext: RequestContext;
  user: AuthenticatedUserInfo;
  deps: ApiDependencies;
}

/**
 * Minimal authenticated user info passed through context.
 */
export interface AuthenticatedUserInfo {
  id: string;
  name: string;
  email: string;
  roles: string[];
  groups: string[];
  tenantId: string;
}

/**
 * Relay-style pagination arguments.
 */
export interface PaginationArgs {
  first?: number;
  after?: string;
  last?: number;
  before?: string;
}

/**
 * Relay-style connection result.
 */
export interface Connection<T> {
  edges: Edge<T>[];
  pageInfo: PageInfo;
  totalCount: number;
}

export interface Edge<T> {
  node: T;
  cursor: string;
}

export interface PageInfo {
  hasNextPage: boolean;
  hasPreviousPage: boolean;
  startCursor: string | null;
  endCursor: string | null;
}

/**
 * Consent purpose used for data access checks.
 */
export type ConsentPurpose = DataPurpose;

/**
 * Default consent purpose for GraphQL queries.
 */
export const DEFAULT_CONSENT_PURPOSE = 'DIRECT_CARE' as const;

/**
 * Default page size when first/last not specified.
 */
export const DEFAULT_PAGE_SIZE = 20;

/**
 * Maximum page size.
 */
export const MAX_PAGE_SIZE = 100;
