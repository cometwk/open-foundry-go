import { describe, it, expect, vi, beforeEach } from 'vitest';
import { applyConsentRecord, generateConsentRoutes } from '../consent/router.js';
import type { ApiDependencies } from '../graphql/types.js';

function mockDeps(withConsent = true, recorderRoles?: readonly string[]) {
  const recordConsent = vi.fn().mockResolvedValue(undefined);
  const auditWrite = vi.fn().mockResolvedValue(undefined);
  const deps = {
    consentService: withConsent ? { recordConsent } : undefined,
    auditWriter: { write: auditWrite },
    ...(recorderRoles ? { consentRecorderRoles: recorderRoles } : {}),
  } as unknown as ApiDependencies;
  return { deps, recordConsent, auditWrite };
}

const ADMIN = { id: 'u1', roles: ['admin'] };
const CLERK = { id: 'u2', roles: ['receptionist'] };
const NURSE = { id: 'u3', roles: ['nurse_in_charge'] };

describe('applyConsentRecord', () => {
  let d: ReturnType<typeof mockDeps>;
  beforeEach(() => { d = mockDeps(); });

  it('records a GRANT for a recorder role and audits it', async () => {
    const r = await applyConsentRecord(d.deps, { subject: 'p-1' }, ADMIN, 'default', 't1');
    expect(r.ok).toBe(true);
    expect(r.data).toMatchObject({ subject: 'p-1', purpose: 'DIRECT_CARE', decision: 'GRANT', recorded: true });
    expect(d.recordConsent).toHaveBeenCalledWith('p-1', 'DIRECT_CARE', 'GRANT', undefined, 'default');
    expect(d.auditWrite).toHaveBeenCalledWith(expect.objectContaining({
      detail: expect.objectContaining({ result: 'success', consentDecision: 'granted' }),
    }));
  });

  it('passes through purpose, DENY decision and evidence', async () => {
    const r = await applyConsentRecord(
      d.deps, { subject: 'p-1', purpose: 'RESEARCH', decision: 'deny', evidence: 'verbal' }, ADMIN, 'default', 't1',
    );
    expect(r.ok).toBe(true);
    expect(d.recordConsent).toHaveBeenCalledWith('p-1', 'RESEARCH', 'DENY', 'verbal', 'default');
  });

  it('denies a non-recorder role (403) and audits the denial', async () => {
    const r = await applyConsentRecord(d.deps, { subject: 'p-1' }, CLERK, 'default', 't1');
    expect(r.ok).toBe(false);
    expect(r.category).toBe('authorization');
    expect(d.recordConsent).not.toHaveBeenCalled();
    expect(d.auditWrite).toHaveBeenCalledWith(expect.objectContaining({
      detail: expect.objectContaining({ result: 'denied' }),
    }));
  });

  it('defaults to admin-only: a clinical role (nurse_in_charge) is denied without config', async () => {
    const r = await applyConsentRecord(d.deps, { subject: 'p-1' }, NURSE, 'default', 't1');
    expect(r.ok).toBe(false);
    expect(r.category).toBe('authorization');
    expect(d.recordConsent).not.toHaveBeenCalled();
  });

  it('honours configured consentRecorderRoles (deps.consentRecorderRoles)', async () => {
    const nd = mockDeps(true, ['admin', 'nurse_in_charge', 'clinician']);
    const r = await applyConsentRecord(nd.deps, { subject: 'p-1' }, NURSE, 'default', 't1');
    expect(r.ok).toBe(true);
    expect(nd.recordConsent).toHaveBeenCalledWith('p-1', 'DIRECT_CARE', 'GRANT', undefined, 'default');
  });

  it('audits consent as a pack-agnostic "consent" object, not "patient"', async () => {
    await applyConsentRecord(d.deps, { subject: 'cust-1' }, ADMIN, 'default', 't1');
    expect(d.auditWrite).toHaveBeenCalledWith(expect.objectContaining({
      operation: expect.objectContaining({ objectType: 'consent', objectId: 'cust-1' }),
    }));
  });

  it('rejects a missing subject', async () => {
    const r = await applyConsentRecord(d.deps, {}, ADMIN, 'default', 't1');
    expect(r.ok).toBe(false);
    expect(r.code).toBe('VALIDATION_ERROR');
  });

  it('rejects an unknown purpose', async () => {
    const r = await applyConsentRecord(d.deps, { subject: 'p-1', purpose: 'MARKETING' }, ADMIN, 'default', 't1');
    expect(r.ok).toBe(false);
    expect(r.code).toBe('INVALID_PURPOSE');
  });

  it('rejects an invalid decision', async () => {
    const r = await applyConsentRecord(d.deps, { subject: 'p-1', decision: 'MAYBE' }, ADMIN, 'default', 't1');
    expect(r.ok).toBe(false);
    expect(r.code).toBe('INVALID_DECISION');
  });

  it('fails when consent service is not configured', async () => {
    const nd = mockDeps(false);
    const r = await applyConsentRecord(nd.deps, { subject: 'p-1' }, ADMIN, 'default', 't1');
    expect(r.ok).toBe(false);
    expect(r.code).toBe('CONSENT_NOT_CONFIGURED');
  });
});

describe('generateConsentRoutes', () => {
  it('maps the core result onto a REST response', async () => {
    const { deps } = mockDeps();
    const routes = generateConsentRoutes(deps);
    const route = routes.find((r) => r.method === 'POST')!;
    const ctx = { user: { id: 'u1', roles: ['admin'] }, requestContext: { tenantId: 'default', traceId: 't1' } } as never;
    const ok = await route.handler({ body: { subject: 'p-1' } } as never, ctx);
    expect(ok.status).toBe(200);
    const denied = await route.handler(
      { body: { subject: 'p-1' } } as never,
      { user: { id: 'u2', roles: ['receptionist'] }, requestContext: { tenantId: 'default', traceId: 't1' } } as never,
    );
    expect(denied.status).toBe(403);
  });
});
