package user

import "testing"

const zeroUUID = "00000000-0000-0000-0000-000000000000"

func TestPreferenceScan_LegacyEmptySelectedOrganizationId(t *testing.T) {
	p := &Preference{}
	if err := p.Scan([]byte(`{"selectedOrganizationId":""}`)); err != nil {
		t.Fatalf("scan with empty selectedOrganizationId should not error, got: %v", err)
	}
	if got := p.SelectedOrganizationId.String(); got != zeroUUID {
		t.Fatalf("expected zero UUID, got %s", got)
	}
}

func TestPreferenceScan_LegacyNonUUIDSelectedOrganizationId(t *testing.T) {
	p := &Preference{}
	if err := p.Scan([]byte(`{"selectedOrganizationId":"not-a-uuid"}`)); err != nil {
		t.Fatalf("scan with non-UUID selectedOrganizationId should not error, got: %v", err)
	}
	if got := p.SelectedOrganizationId.String(); got != zeroUUID {
		t.Fatalf("expected zero UUID, got %s", got)
	}
}

func TestPreferenceScan_ValidSelectedOrganizationIdPreserved(t *testing.T) {
	id := "00000000-0000-0000-0000-000000000001"
	p := &Preference{}
	if err := p.Scan([]byte(`{"selectedOrganizationId":"` + id + `"}`)); err != nil {
		t.Fatalf("scan with valid UUID should not error, got: %v", err)
	}
	if got := p.SelectedOrganizationId.String(); got != id {
		t.Fatalf("expected %s, got %s", id, got)
	}
}
