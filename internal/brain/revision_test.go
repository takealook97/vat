package brain

import "testing"

// Two commands ask whether a pinned revision still names what the repository is
// at, and they used to disagree: `vat lint` compared by prefix, promotion
// compared exactly. A record written with a short hash was therefore current to
// one and moved to the other, and the reviewer paid the difference in a
// --reverified they did not owe. One comparison, tested here, is the fix.

func TestSameRevisionAcceptsAnAbbreviationGitWouldAccept(t *testing.T) {
	// Arrange
	full := "3f9a1c2e8b7461d05a2c9e8f4b10d7c6a5e34210"

	// Act & Assert
	for _, pinned := range []string{full, full[:7], full[:12], full[:4]} {
		if !SameRevision(pinned, full) {
			t.Errorf("SameRevision(%q, %q) = false; an abbreviation of the same commit is the same evidence", pinned, full)
		}
	}
}

func TestSameRevisionRefusesAPrefixTooShortToMeanAnything(t *testing.T) {
	// Arrange: below git's own four-character minimum a prefix match says
	// almost nothing, and reporting evidence as unchanged because two hashes
	// begin alike is the failure this guard exists for.
	full := "3f9a1c2e8b7461d05a2c9e8f4b10d7c6a5e34210"

	// Act & Assert
	for _, pinned := range []string{"", "3", "3f", "3f9"} {
		if SameRevision(pinned, full) {
			t.Errorf("SameRevision(%q, %q) = true; that is not an identification", pinned, full)
		}
	}
}

func TestSameRevisionRefusesAnUnknownCurrentRevision(t *testing.T) {
	// Arrange: vat could not read the repository. That is a reason to ask the
	// reviewer, never a reason to conclude the evidence held.
	if SameRevision("3f9a1c2e8b74", "") {
		t.Error("an unreadable repository was treated as confirmation that nothing moved")
	}
}

func TestSameRevisionRefusesADifferentCommit(t *testing.T) {
	if SameRevision("3f9a1c2e8b74", "9d4e7b1a0c62f83b1e0a7c5d2b64f9081a3c7e55") {
		t.Error("two different commits were reported as the same evidence")
	}
}

func TestPromoteAcceptsAnAbbreviatedPinThatStillNamesHead(t *testing.T) {
	// Arrange
	root := newStore(t)
	mustCreate(t, root, NewRecordInput{
		Kind: KindGap, ID: "G-0001", Title: "Retries double-submit",
		Status: StatusProvisional, ClaimKind: ClaimCurrentState,
		OwnedBy: "payments", SourceRef: "payments@3f9a1c2e8b74:docs/ORDERING.md",
	})
	record := recordByID(t, mustLoad(t, root), "G-0001")

	// Act
	err := Promote(root, record, PromoteRequest{
		Reviewer: "alex", Now: longAfter,
		SourceRevision: "3f9a1c2e8b7461d05a2c9e8f4b10d7c6a5e34210",
	})

	// Assert
	if err != nil {
		t.Fatalf("promote refused a claim whose evidence never moved: %v", err)
	}
	after := recordByID(t, mustLoad(t, root), "G-0001")
	if after.SourceRef != "payments@3f9a1c2e8b74:docs/ORDERING.md" {
		t.Errorf("source_ref = %q; unchanged evidence must not be re-pinned", after.SourceRef)
	}
}
