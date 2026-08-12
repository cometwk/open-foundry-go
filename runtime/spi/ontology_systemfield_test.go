package spi

import "testing"

// TestReservedFieldConstantValues pins the wire string value of every
// reserved-field constant. These constants are the single source of truth
// for the _-prefixed storage-wire columns, so any value drift is a
// byte-level wire-shape change. R1.
func TestReservedFieldConstantValues(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"FieldID", FieldID, "_id"},
		{"FieldType", FieldType, "_type"},
		{"FieldTenantID", FieldTenantID, "_tenantId"},
		{"FieldVersion", FieldVersion, "_version"},
		{"FieldCreatedAt", FieldCreatedAt, "_createdAt"},
		{"FieldUpdatedAt", FieldUpdatedAt, "_updatedAt"},
		{"FieldDeletedAt", FieldDeletedAt, "_deletedAt"},
		{"LinkFieldFromID", LinkFieldFromID, "_fromId"},
		{"LinkFieldToID", LinkFieldToID, "_toId"},
		{"LinkFieldFromType", LinkFieldFromType, "_fromType"},
		{"LinkFieldToType", LinkFieldToType, "_toType"},
		{"LinkFieldEngineLinkID", LinkFieldEngineLinkID, "_engineLinkId"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q (wire shape drift)", c.name, c.got, c.want)
		}
	}
}

// TestIsSystemField asserts the object reserved-field membership set: the
// seven object reserved fields are true, the five link-only fields are
// false (they belong to IsLinkSystemField, not IsSystemField), and a user
// field is false. R2.
func TestIsSystemField(t *testing.T) {
	objectReserved := []string{
		FieldID, FieldType, FieldTenantID, FieldVersion,
		FieldCreatedAt, FieldUpdatedAt, FieldDeletedAt,
	}
	for _, k := range objectReserved {
		if !IsSystemField(k) {
			t.Errorf("IsSystemField(%q) = false, want true (object reserved field)", k)
		}
	}
	linkOnly := []string{
		LinkFieldFromID, LinkFieldToID, LinkFieldFromType, LinkFieldToType, LinkFieldEngineLinkID,
	}
	for _, k := range linkOnly {
		if IsSystemField(k) {
			t.Errorf("IsSystemField(%q) = true, want false (link-only field, not object reserved)", k)
		}
	}
	if IsSystemField("name") {
		t.Errorf(`IsSystemField("name") = true, want false (user field)`)
	}
}

// TestIsLinkSystemField asserts the link reserved-field membership set:
// all twelve reserved fields (seven object + five link-specific) are true
// because IsLinkSystemField delegates to IsSystemField, and a user field
// is false. R2.
func TestIsLinkSystemField(t *testing.T) {
	allReserved := []string{
		FieldID, FieldType, FieldTenantID, FieldVersion,
		FieldCreatedAt, FieldUpdatedAt, FieldDeletedAt,
		LinkFieldFromID, LinkFieldToID, LinkFieldFromType, LinkFieldToType, LinkFieldEngineLinkID,
	}
	for _, k := range allReserved {
		if !IsLinkSystemField(k) {
			t.Errorf("IsLinkSystemField(%q) = false, want true (reserved on links)", k)
		}
	}
	if IsLinkSystemField("name") {
		t.Errorf(`IsLinkSystemField("name") = true, want false (user field)`)
	}
}

// TestIsSystemField_EmptyAndUnknown covers the edge cases: an empty key is
// not reserved, and a future-reserved candidate like "_tenant" (not in
// either set today) returns false from both helpers so new reserved fields
// cannot be added accidentally by prefix-matching. R3 edge coverage.
func TestIsSystemField_EmptyAndUnknown(t *testing.T) {
	if IsSystemField("") {
		t.Errorf(`IsSystemField("") = true, want false (empty key is not reserved)`)
	}
	if IsLinkSystemField("") {
		t.Errorf(`IsLinkSystemField("") = true, want false (empty key is not reserved)`)
	}
	if IsSystemField("_tenant") {
		t.Errorf(`IsSystemField("_tenant") = true, want false (future-reserved candidate, not in set)`)
	}
	if IsLinkSystemField("_tenant") {
		t.Errorf(`IsLinkSystemField("_tenant") = true, want false (future-reserved candidate, not in set)`)
	}
}