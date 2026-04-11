package responsepayload

import (
	"encoding/json"
	"testing"
)

func TestUsageResultMarshal(t *testing.T) {
	t.Parallel()

	got, err := json.Marshal(UsageResult{Usage: "help text"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(got) != `{"usage":"help text"}` {
		t.Fatalf("unexpected json: %s", got)
	}
}

func TestJavaToolchainsMarshal(t *testing.T) {
	t.Parallel()

	got, err := json.Marshal(JavaToolchains{
		Java: JavaToolchainInfo{JavaHome: "/tmp/java"},
		Kotlin: KotlinToolchainInfo{
			Kotlinc: "/tmp/kotlinc",
			Plugins: KotlinToolchainPlugins{
				Compose:       "/tmp/compose.jar",
				Serialization: "/tmp/serialization.jar",
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(got) != `{"java":{"javaHome":"/tmp/java"},"kotlin":{"kotlinc":"/tmp/kotlinc","plugins":{"compose":"/tmp/compose.jar","serialization":"/tmp/serialization.jar"}}}` {
		t.Fatalf("unexpected json: %s", got)
	}
}

func TestCacheProbeMarshal(t *testing.T) {
	t.Parallel()

	got, err := json.Marshal(CacheProbe{
		ActionID: "action:compile",
		State:    "reused",
		Basis:    "cache-probes",
		Detail:   "1 cache hit, 0 cache misses",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(got) != `{"actionId":"action:compile","state":"reused","basis":"cache-probes","detail":"1 cache hit, 0 cache misses"}` {
		t.Fatalf("unexpected json: %s", got)
	}
}

func TestCacheProbeRecordMarshal(t *testing.T) {
	t.Parallel()

	got, err := json.Marshal(CacheProbeRecord{
		ActionID: "action:compile",
		StepName: "compileKotlin",
		Order:    1,
		State:    "reused",
		Basis:    "shared-cache-hit",
		Detail:   "restored compiled classes from shared cache",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(got) != `{"actionId":"action:compile","stepName":"compileKotlin","order":1,"state":"reused","basis":"shared-cache-hit","detail":"restored compiled classes from shared cache"}` {
		t.Fatalf("unexpected json: %s", got)
	}
}
