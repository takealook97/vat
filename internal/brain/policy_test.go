package brain_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/takealook97/vat/internal/brain"
)

func writeMarker(t *testing.T, content string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, brain.MarkerFile), []byte(content), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	return root
}

func TestMarkerPolicyDefaultsAndValidation(t *testing.T) {
	defaults := brain.DefaultMarkerPolicy()
	if defaults != (brain.MarkerPolicy{PromotionGate: "manual", StaleAfterDays: 90, ReviewSLADays: 30, RequirePromotionGate: true}) {
		t.Fatalf("defaults changed: %+v", defaults)
	}

	t.Run("a marker without a policy declares nothing and resolves to the defaults", func(t *testing.T) {
		root := writeMarker(t, "# comment\nschema: 1\n")
		declared, err := brain.ReadMarkerPolicy(root)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if len(declared) != 0 {
			t.Errorf("declared %v from a marker with no policy", declared)
		}
		if declared.Resolve() != defaults {
			t.Errorf("resolved %+v, want the defaults", declared.Resolve())
		}
	})

	t.Run("a missing marker declares nothing", func(t *testing.T) {
		declared, err := brain.ReadMarkerPolicy(t.TempDir())
		if err != nil || len(declared) != 0 {
			t.Fatalf("declared %v, err %v", declared, err)
		}
	})

	t.Run("declared keys win and absent ones default, comments allowed", func(t *testing.T) {
		root := writeMarker(t, "# a brain\nschema: 1\npolicy:\n  # stricter here\n  stale_after_days: 30 # a month\n  promotion_gate: agent\n")
		declared, err := brain.ReadMarkerPolicy(root)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if got := declared.Keys(); !reflect.DeepEqual(got, []string{"promotion_gate", "stale_after_days"}) {
			t.Errorf("declared keys %v", got)
		}
		want := brain.MarkerPolicy{PromotionGate: "agent", StaleAfterDays: 30, ReviewSLADays: 30, RequirePromotionGate: true}
		if declared.Resolve() != want {
			t.Errorf("resolved %+v, want %+v", declared.Resolve(), want)
		}
		if version, ok := brain.DeclaredSchema(root); !ok || version != 1 {
			t.Errorf("the schema line stopped reading: %d %v", version, ok)
		}
	})

	t.Run("every invalid value is reported at once", func(t *testing.T) {
		root := writeMarker(t, "schema: 1\npolicy:\n  promotion_gate: sometimes\n  stale_after_days: 0\n  review_sla_days: soon\n  require_promotion_gate: maybe\n  owner: payments\n")
		_, err := brain.ReadMarkerPolicy(root)
		if err == nil {
			t.Fatal("an invalid policy was accepted")
		}
		for _, key := range []string{"promotion_gate", "stale_after_days", "review_sla_days", "require_promotion_gate", "policy.owner"} {
			if !strings.Contains(err.Error(), key) {
				t.Errorf("the error does not name %s: %v", key, err)
			}
		}
	})

	t.Run("a marker that is not YAML is refused", func(t *testing.T) {
		root := writeMarker(t, "schema: 1\npolicy: [unclosed\n")
		if _, err := brain.ReadMarkerPolicy(root); err == nil {
			t.Fatal("a broken marker was accepted")
		}
	})

	t.Run("a standalone scaffold writes the policy it runs under", func(t *testing.T) {
		root := t.TempDir()
		if _, err := brain.InitWithPolicy(root, reference, &defaults); err != nil {
			t.Fatalf("init: %v", err)
		}
		declared, err := brain.ReadMarkerPolicy(root)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if len(declared) != len(brain.PolicyKeys()) || declared.Resolve() != defaults {
			t.Errorf("scaffolded marker declares %v", declared)
		}
		if version, ok := brain.DeclaredSchema(root); !ok || version != brain.SchemaVersion {
			t.Errorf("schema line lost: %d %v", version, ok)
		}
	})

	t.Run("the check policy carries the clock", func(t *testing.T) {
		policy := defaults.CheckPolicy()
		if policy.StaleAfterDays != 90 || policy.ReviewSLADays != 30 || policy.PromotionGate != "manual" {
			t.Errorf("check policy %+v", policy)
		}
	})
}
