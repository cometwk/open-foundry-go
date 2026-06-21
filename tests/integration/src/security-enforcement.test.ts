/**
 * Security-enforcement E2E (NODE_ENV=production).
 *
 * The standard integration suite runs the gateway in development mode, where
 * authentication is bypassed (synthetic dev-user) and authorization is an
 * allow-all stub — so OIDC validation, the OpenFGA model, and the listObjects
 * authz filter are NEVER exercised end-to-end. This test boots the gateway in
 * production mode against real Keycloak (OIDC) + OpenFGA and asserts the
 * enforcement that dev mode hides:
 *
 *   - no / invalid bearer token  → 401 (authentication enforced)
 *   - a valid token performs an authorized action (RegisterPatient as a
 *     clinician) → success (proves OIDC + role authz)
 *   - the authenticated clinician then lists patients and CANNOT see the
 *     patient it just created (no `viewer` tuple — patient is not admitted),
 *     and a direct read of it is 403 (proves the OpenFGA listObjects/check
 *     filter, vs dev allow-all which would return it).
 *
 * Production prerequisites the harness wires up in beforeAll:
 *   1. bring up Keycloak (realm `openfoundry` auto-imported with users
 *      dr-test/admin-test) + OpenFGA, on the base compose (dev-mode gateway not
 *      yet started);
 *   2. create an OpenFGA store (the gateway needs OPENFGA_STORE_ID at boot);
 *   3. bring the gateway up with the prod-test override (NODE_ENV=production +
 *      OIDC + the store id) so it syncs the authz model into that store;
 *   4. mint a real password-grant token for dr-test (public client, direct
 *      access grants).
 *
 * DESTRUCTIVE + ISOLATED: owns the Docker stack lifecycle and boots it in a
 * non-default (production) mode on the same ports, so it is gated behind
 * SECURITY_E2E=1 and must run on its OWN — never alongside the standard suite.
 * After it completes the stack is DOWN; bring the normal dev stack back up
 * before running the rest of the suite. Run with, e.g.:
 *
 *   SECURITY_E2E=1 pnpm --filter @openfoundry/integration-tests exec \
 *     vitest run security-enforcement
 */

import { describe, it, expect, beforeAll, afterAll } from 'vitest';
import { execSync } from 'node:child_process';
import { resolve, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { dockerAvailable } from './setup.js';
import { waitForEndpoint } from './docker.js';
import { CONFIG } from './config.js';

const ENABLED = process.env['SECURITY_E2E'] === '1';
const describeMaybe = dockerAvailable && ENABLED ? describe : describe.skip;

const DEPLOY_DIR = resolve(dirname(fileURLToPath(import.meta.url)), '../../../deploy');
const BASE = `${DEPLOY_DIR}/docker-compose.yaml`;
const PROD = `${DEPLOY_DIR}/docker-compose.prod-test.yaml`;

const OPENFGA_URL = 'http://localhost:8280';
const KEYCLOAK_TOKEN_URL =
  'http://localhost:8180/auth/realms/openfoundry/protocol/openid-connect/token';
const HEALTH_URL = `${CONFIG.apiBaseUrl}/.well-known/apollo/server-health`;
const PATIENTS_URL = `${CONFIG.restBaseUrl}/patients`;

function compose(files: string[], cmd: string, env?: Record<string, string>): void {
  const fileArgs = files.map((f) => `-f "${f}"`).join(' ');
  execSync(`docker compose ${fileArgs} ${cmd}`, {
    stdio: 'inherit',
    env: { ...process.env, ...env },
  });
}

/** Create an OpenFGA store and return its id. */
async function createOpenFgaStore(): Promise<string> {
  const res = await fetch(`${OPENFGA_URL}/stores`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ name: 'security-e2e' }),
  });
  if (!res.ok) throw new Error(`OpenFGA store creation failed: ${res.status} ${await res.text()}`);
  const body = (await res.json()) as { id: string };
  return body.id;
}

/** Mint a password-grant access token for a realm user (public client). */
async function mintToken(username: string, password = 'test-password'): Promise<string> {
  const form = new URLSearchParams({
    grant_type: 'password',
    client_id: 'openfoundry',
    username,
    password,
    scope: 'openid',
  });
  // Keycloak realm import can lag behind the master-realm healthcheck; retry.
  let lastErr = '';
  for (let attempt = 0; attempt < 20; attempt++) {
    const res = await fetch(KEYCLOAK_TOKEN_URL, {
      method: 'POST',
      headers: { 'content-type': 'application/x-www-form-urlencoded' },
      body: form,
    });
    if (res.ok) {
      const body = (await res.json()) as { access_token: string };
      return body.access_token;
    }
    lastErr = `${res.status} ${await res.text()}`;
    await new Promise((r) => setTimeout(r, 3_000));
  }
  throw new Error(`Token mint for ${username} failed after retries: ${lastErr}`);
}

/** 10-digit NHS number with a valid mod-11 check digit (nhsNumber is @unique). */
function makeNhsNumber(seed: number): string {
  for (let s = seed; ; s++) {
    const base = String(s).padStart(9, '0').slice(-9);
    let sum = 0;
    for (let i = 0; i < 9; i++) sum += Number(base[i]) * (10 - i);
    const check = 11 - (sum % 11);
    const checkDigit = check === 11 ? 0 : check;
    if (checkDigit !== 10) return base + String(checkDigit);
  }
}

const auth = (token: string) => ({ authorization: `Bearer ${token}` });

describeMaybe('security enforcement (production mode)', () => {
  let drToken: string;
  let createdPatientId: string;

  beforeAll(async () => {
    // 1. Clean slate, then bring up Keycloak + OpenFGA (base compose only — the
    //    dev-mode gateway must NOT start; the prod gateway needs a store id).
    compose([BASE], 'down -v --remove-orphans');
    compose([BASE], 'up -d --wait keycloak openfga');

    // 2. Wait for OpenFGA (no healthcheck → poll) and create a store.
    await waitForEndpoint(`${OPENFGA_URL}/healthz`, 60, 2_000);
    const storeId = await createOpenFgaStore();

    // 3. Bring the gateway up in production with the store id; it syncs the
    //    authz model into the store at boot.
    compose([BASE, PROD], 'up -d --wait', { OPENFGA_STORE_ID: storeId });
    await waitForEndpoint(HEALTH_URL, 90, 3_000);

    // 4. Mint a real clinician token.
    drToken = await mintToken('dr-test');
  }, 600_000);

  afterAll(() => {
    compose([BASE], 'down -v --remove-orphans');
  });

  it('rejects an unauthenticated request (401)', async () => {
    const res = await fetch(PATIENTS_URL);
    expect(res.status).toBe(401);
  });

  it('rejects an invalid bearer token (401)', async () => {
    const res = await fetch(PATIENTS_URL, { headers: auth('not-a-real-token') });
    expect(res.status).toBe(401);
  });

  it('rejects an unauthenticated action (401)', async () => {
    const res = await fetch(`${CONFIG.restBaseUrl}/actions/RegisterPatient`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ nhsNumber: makeNhsNumber(1), name: 'X', dateOfBirth: '1990-01-01' }),
    });
    expect(res.status).toBe(401);
  });

  it('admits a valid token and performs an authorized action (RegisterPatient as clinician)', async () => {
    const res = await fetch(`${CONFIG.restBaseUrl}/actions/RegisterPatient`, {
      method: 'POST',
      headers: { 'content-type': 'application/json', ...auth(drToken) },
      body: JSON.stringify({
        nhsNumber: makeNhsNumber(Date.now() % 1_000_000_000),
        name: 'Prod Security Test',
        dateOfBirth: '1985-03-12',
        // Skip the consent side-effect; not under test here.
        consent: false,
      }),
    });
    expect(res.status).toBe(200);
    const body = (await res.json()) as {
      data: { success: boolean; affectedObjects: { typeName: string; id: string }[] };
    };
    expect(body.data.success).toBe(true);
    const created = body.data.affectedObjects.find((o) => o.typeName === 'Patient');
    expect(created).toBeDefined();
    createdPatientId = created!.id;
  });

  it('filters the list: the clinician cannot see the patient it created (no viewer tuple)', async () => {
    const res = await fetch(PATIENTS_URL, { headers: auth(drToken) });
    expect(res.status).toBe(200);
    const body = (await res.json()) as {
      data: { id: string }[];
      pagination: { totalCount: number };
    };
    // The patient exists (created above) but is not admitted to any ward, so the
    // clinician has no `viewer` path to it — the OpenFGA listObjects filter
    // excludes it. In dev allow-all this list would contain it.
    expect(body.data.some((p) => p.id === createdPatientId)).toBe(false);
    expect(body.pagination.totalCount).toBe(0);
  });

  it('denies a direct read of the unauthorized patient (403)', async () => {
    const res = await fetch(`${PATIENTS_URL}/${createdPatientId}`, { headers: auth(drToken) });
    expect(res.status).toBe(403);
  });
});
