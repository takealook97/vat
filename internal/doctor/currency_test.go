package doctor_test

import (
	"context"
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/doctor"
	"github.com/takealook97/vat/internal/manifest"
)

// `requires.vat` answers whether this binary is too old for this workspace. It
// cannot answer whether a newer one exists: a workspace pinned `>=0.6.1
// <0.7.0` is satisfied by 0.6.1 forever, so every release after it arrives
// unannounced to the people who declared that range. This is the other
// question, and `vat doctor` is where it belongs — it reports, and repairs
// nothing.
func TestABinaryBehindTheNewestReleaseIsReported(t *testing.T) {
	// Arrange
	ws := fixture(t, manifest.Repo{
		Name: "payments", Origin: "https://example.invalid/acme/payments.git",
		Role: manifest.RoleProduct,
	})

	// Act
	report := doctor.Run(context.Background(), ws, doctor.Options{
		Now: reference, ToolVersion: "v0.6.1", LatestVersion: "v0.6.2",
	})

	// Assert
	finding, found := find(report, "tools", "vat")
	if !found {
		t.Fatal("a binary a release behind was not reported at all")
	}
	if finding.Status != doctor.StatusWarn {
		t.Errorf("status = %q, want warn", finding.Status)
	}
	if !strings.Contains(finding.Detail, "v0.6.2") {
		t.Errorf("detail = %q; it does not name the release that is available", finding.Detail)
	}
}

func TestABinaryAtTheNewestReleaseIsNotReportedAsBehind(t *testing.T) {
	// Arrange: the other side. A tool that reports every healthy run as needing
	// attention is one people stop reading.
	ws := fixture(t, manifest.Repo{
		Name: "payments", Origin: "https://example.invalid/acme/payments.git",
		Role: manifest.RoleProduct,
	})

	// Act
	report := doctor.Run(context.Background(), ws, doctor.Options{
		Now: reference, ToolVersion: "v0.6.2", LatestVersion: "v0.6.2",
	})

	// Assert
	finding, found := find(report, "tools", "vat")
	if !found {
		t.Fatal("the running version was not reported at all")
	}
	if finding.Status != doctor.StatusOK {
		t.Errorf("status = %q, want ok", finding.Status)
	}
}

// Offline, behind a proxy that refuses, or a build carrying no release version
// at all. None of those is a finding: the question simply was not answered, and
// inventing a warning out of a failed lookup is how a check earns its way into
// being ignored.
func TestNoAnswerAboutTheNewestReleaseIsNotAFinding(t *testing.T) {
	// Arrange
	ws := fixture(t, manifest.Repo{
		Name: "payments", Origin: "https://example.invalid/acme/payments.git",
		Role: manifest.RoleProduct,
	})

	for _, unanswered := range []doctor.Options{
		{Now: reference, ToolVersion: "v0.6.2"},
		{Now: reference, LatestVersion: "v0.6.2"},
		{Now: reference, ToolVersion: "dev", LatestVersion: "v0.6.2"},
	} {
		// Act
		report := doctor.Run(context.Background(), ws, unanswered)

		// Assert
		if _, found := find(report, "tools", "vat"); found {
			t.Errorf("a lookup that produced no answer was reported as one: %+v", unanswered)
		}
	}
}
