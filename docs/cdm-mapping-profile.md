# FDP/CDM Compatibility Profile (S1.0)

Human-readable canonical mapping document for the NHS acute operational subset.
This is the companion to the machine-readable profile in
`packages/api/src/cdm/profile.ts` and the projection served at `/api/v1/cdm/*`.

## What this is (and is not)

Open Foundry does **not** embed the NHS Federated Data Platform Canonical Data
Model (CDM). It ships a **declarative mapping profile** that projects the ODL
operational ontology into a CDM-shaped, read-only view, preserving provenance
end-to-end. This converts "another open-source Foundry" into an
**FDP-compatible runtime** — the cheapest, highest-leverage interoperability
artifact (plan §S1.0).

The CDM target is the NHS England standard **DAPB4121** (draft-in-progress).
This profile pins a placeholder revision label (`fdp-cdm-draft`) and records the
snapshot + revalidation cadence in the compatibility matrix below; the patient
data exercised in tests is synthetic, but the CDM target is not invented.

## Compatibility matrix

| Open Foundry | Profile version | CDM revision | CDM status |
|---|---|---|---|
| `nhs-acute` 0.2.0 | 0.1.0 | `fdp-cdm-draft` | DAPB4121 draft-in-progress; revalidate quarterly |

Profile version is independent of the platform and spec version tracks. When the
upstream CDM revises, bump `cdmVersion` + `profileVersion` and re-run the gap
review.

## Operational subset

Patient, Ward, Bed, Admission, Discharge, Transfer, Staff, Encounter.

Coverage caveats, recorded in the gap register rather than fabricated:
- **Admission** is surfaced as **Encounter** (projected from the `AdmittedTo` link); there is no separate `/Admission` route.
- **Transfer** is a first-class stored object (v0.2.0 B1), written by the `TransferWard` action and exposed as the CDM `Transfer` resource.
- **Staff** is consultant-only (only `Consultant` is modelled).

## Resource mappings

Records are addressed by Open Foundry **source type** for unambiguous routing
(Ward and Bed both project to CDM `Location`). Each record's `resourceType`
field carries the CDM resource name.

### Patient → CDM `Patient`
| CDM field | Source field | Notes |
|---|---|---|
| `id` | `_id` | |
| `nhsNumber` | `nhsNumber` | Provisional-identity flagging is an upstream connector concern |
| `name` | `name` | Full display name; structured components carried in `family`/`given` |
| `family` | `family` | Surname (structured-name decomposition, v0.2.0 B2) |
| `given` | `given` | **Lossy** — one or more forenames, space-separated; consumers split on whitespace |
| `birthDate` | `dateOfBirth` | |
| `status` | `status` | **Lossy** enum remap; `TRANSFERRED` collapses to `active` |
| `triageCategory` | `triageCategory` | **Lossy** — NHS-local P1–P4, not CDM-coded |

### Ward → CDM `Location` (kind=ward)
| CDM field | Source field | Notes |
|---|---|---|
| `id` | `_id` | |
| `kind` | _(constant)_ | `"ward"` |
| `name` | `name` | |
| `specialty` | `specialty` | |
| `capacity` | `capacity` | |

### Bed → CDM `Location` (kind=bed)
| CDM field | Source field | Notes |
|---|---|---|
| `id` | `_id` | |
| `kind` | _(constant)_ | `"bed"` |
| `identifier` | `number` | |
| `bedType` | `type` | |
| `status` | `status` | **Lossy** — `CLEANING` and `OUT_OF_SERVICE` both → `unavailable` |

### Consultant → CDM `Practitioner`
| CDM field | Source field | Notes |
|---|---|---|
| `id` | `_id` | |
| `gmcNumber` | `gmcNumber` | |
| `name` | `name` | **Lossy** — single free-text string |
| `specialty` | `specialty` | |

### DischargeRecord → CDM `Discharge`
| CDM field | Source field | Notes |
|---|---|---|
| `id` | `_id` | |
| `patient` | `patient` | |
| `location` | `ward` | |
| `destination` | `destination` | enum remap (`HOME`→`home`, …) |
| `dischargeDate` | `dischargeDate` | |
| `notes` | `notes` | **Lossy** — free-text |

### Transfer → CDM `Transfer`
First-class record written by the `TransferWard` action (v0.2.0 B1). `fromWard`
is the origin ward (the action's effect snapshot resolves `patient.currentWard`
before the bed/link moves).
| CDM field | Source field | Notes |
|---|---|---|
| `id` | `_id` | |
| `patient` | `patient` | |
| `sourceLocation` | `fromWard` | Origin ward |
| `destinationLocation` | `toWard` | Destination ward |
| `transferDate` | `transferDate` | |
| `reason` | `reason` | **Lossy** — free-text |

### AdmittedTo (link) → CDM `Encounter`
Derived from the `AdmittedTo` link, mirroring the FHIR Encounter projection.
| CDM field | Source field | Notes |
|---|---|---|
| `id` | `_id` | |
| `patient` | `patientId` | |
| `location` | `wardId` | |
| `admissionDate` | `admissionDate` | |
| `expectedDischarge` | `expectedDischarge` | |
| `reason` | `reason` | **Lossy** — free-text, not terminology-coded |
| `status` | `status` | Derived: `ACTIVE`→`in-progress`, `DISCHARGED`→`finished` (from link soft-delete) |

## Provenance

Every projected record carries a `_provenance` envelope so an analyst can see
what was projected and what was approximated:

```json
{
  "sourceType": "Patient",
  "sourceId": "p-1",
  "sourceVersion": 3,
  "sourceUpdatedAt": "2026-05-25T10:00:00.000Z",
  "profileVersion": "0.1.0",
  "cdmVersion": "fdp-cdm-draft",
  "lossyFields": ["name", "status", "triageCategory"]
}
```

## Gap register

| Area | Issue | Fallback |
|---|---|---|
| Admission | Not a distinct resource — surfaced as Encounter via the `AdmittedTo` link; no `/Admission` route | Treat Encounter as the admission record |
| Transfer | **Resolved (v0.2.0 B1)** — `TransferWard` now writes a first-class `Transfer` object, projected as CDM `Transfer` | No longer a gap; transfers are queryable directly and via the CDM projection |
| Staff | Only Consultant is modelled; no nurses/AHPs/admin staff | Extend `nhs-acute` with a general Staff/Practitioner type before claiming full CDM staff coverage |
| Patient.name | **Resolved (v0.2.0 B2)** — Patient carries structured `family` + `given` alongside the full `name` | `given` holds space-separated forenames (split for the list form); `prefix`/`suffix` remain out of scope |
| Patient.identifier | NHS Number optional; local-number-only patients not flagged provisional | Provisional-identity flagging handled upstream (PDS resolution, connector layer) |
| Terminology | Coded fields are free strings / local enums, not validated against SNOMED CT / dm+d / ODS | Terminology validation added at connector layer (S1.2) and full CDM coverage (S2.2) |

## API

Read-only; passes through the same auth / redaction / consent pipeline as FHIR
and GraphQL.

| Endpoint | Description | Auth |
|---|---|---|
| `GET /api/v1/cdm/metadata` | Profile, compatibility matrix, gap register | Public |
| `GET /api/v1/cdm/{SourceType}` | List projection (Patient, Ward, Bed, Consultant, DischargeRecord, Transfer) | Required |
| `GET /api/v1/cdm/{SourceType}/{id}` | Single record projection | Required |
| `GET /api/v1/cdm/Encounter?patient={id}` | Admissions for a patient (via AdmittedTo) | Required |

Patient and Encounter projections are consent-gated (subject = patient).
`Ward`/`Bed`/`Consultant`/`DischargeRecord` are authorization + redaction gated.

## Status

This is a **Stage 1 starter slice**: the profile, projection, provenance, and
read API are complete and tested for the operational subset. A first-class
Transfer object (B1) and structured-name decomposition (B2) landed in v0.2.0.
Full CDM coverage and terminology validation are scoped to later stages (S2.2).
