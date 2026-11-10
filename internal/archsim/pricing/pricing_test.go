package pricing

import (
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/archdoc"
)

func TestTablesComplete(t *testing.T) {
	ts := Tables()
	if len(ts) != 3 || ts[0].Provider != AWS || ts[1].Provider != GCP || ts[2].Provider != Azure {
		t.Fatalf("Tables order = %v", ts)
	}
	asOf := regexp.MustCompile(`^\d{4}-\d{2}$`)
	for _, tb := range ts {
		wantRegions, minTypes := 2, 8
		if tb.Provider == AWS {
			wantRegions, minTypes = 4, 28
			if got := Regions(AWS); !slices.Equal(got, []string{"ap-northeast-1", "ap-northeast-2", "eu-central-1", "us-east-1"}) {
				t.Fatalf("aws regions = %v", got)
			}
		}
		if len(tb.Regions) != wantRegions {
			t.Fatalf("%s: %d regions", tb.Provider, len(tb.Regions))
		}
		if !asOf.MatchString(tb.AsOf) {
			t.Fatalf("%s: AsOf %q", tb.Provider, tb.AsOf)
		}
		if len(tb.Sources) == 0 {
			t.Fatalf("%s: no sources", tb.Provider)
		}
		for _, s := range tb.Sources {
			if !strings.HasPrefix(s, "https://") {
				t.Fatalf("%s: source %q", tb.Provider, s)
			}
		}
		for code, r := range tb.Regions {
			if r.Code != code {
				t.Fatalf("%s %s: Code %q", tb.Provider, code, r.Code)
			}
			if len(r.Instances) < minTypes {
				t.Fatalf("%s %s: %d instance types", tb.Provider, code, len(r.Instances))
			}
			for name, it := range r.Instances {
				if it.Name != name || it.HourlyMicroUSD <= 0 || it.VCPU <= 0 || it.MemoryMiB <= 0 || it.NICMbps <= 0 {
					t.Fatalf("%s %s: bad instance %+v", tb.Provider, code, it)
				}
			}
			for _, dt := range archdoc.DiskTypes() {
				if r.DiskGBMonthMicroUSD[dt] <= 0 {
					t.Fatalf("%s %s: disk %s price %d", tb.Provider, code, dt, r.DiskGBMonthMicroUSD[dt])
				}
			}
			for name, v := range map[string]int64{"iops": r.IOPSMonthMicroUSD, "egress": r.EgressGBMicroUSD, "lb hour": r.LBHourlyMicroUSD,
				"lb gb": r.LBGBMicroUSD, "object": r.ObjectGBMonthMicroUSD} {
				if v <= 0 {
					t.Fatalf("%s %s: %s price %d", tb.Provider, code, name, v)
				}
			}
			for kind, typ := range r.Managed {
				if _, ok := r.Instances[typ]; !ok {
					t.Fatalf("%s %s: managed %s → %q not in instances", tb.Provider, code, kind, typ)
				}
			}
		}
	}
	if AsOf() != "2026-11" {
		t.Fatalf("AsOf = %q", AsOf())
	}
}

func TestLookupC7gXlarge(t *testing.T) {
	it, ok := Lookup(AWS, "ap-northeast-2", "c7g.xlarge")
	if !ok || it.VCPU != 4 || it.MemoryMiB != 8192 || it.NICMbps != 12500 || it.HourlyMicroUSD <= 0 {
		t.Fatalf("Lookup = %+v, %v", it, ok)
	}
	if _, ok := Lookup(AWS, "mars-1", "c7g.xlarge"); ok {
		t.Fatal("unknown region found")
	}
	if _, ok := Lookup(Custom, "", "x"); ok {
		t.Fatal("custom provider has no table")
	}
	list := Instances(AWS, "us-east-1")
	if !slices.IsSortedFunc(list, func(a, b InstanceType) int { return strings.Compare(a.Name, b.Name) }) || len(list) < 28 {
		t.Fatalf("Instances not sorted or short: %d", len(list))
	}
	if Instances(GCP, "nowhere") != nil || Regions(Custom) != nil {
		t.Fatal("unknown lookups must be empty")
	}
	if _, ok := RegionOf(Azure, "koreacentral"); !ok {
		t.Fatal("azure koreacentral missing")
	}
}

func TestDefaultTypePerKind(t *testing.T) {
	for _, tb := range Tables() {
		for _, k := range archdoc.NodeKinds() {
			typ := DefaultType(tb.Provider, k)
			switch k {
			case archdoc.KindClient, archdoc.KindCDN, archdoc.KindExternal:
				if typ != "" {
					t.Fatalf("%s %s: default type %q", tb.Provider, k, typ)
				}
				continue
			}
			if typ == "" {
				t.Fatalf("%s %s: no default type", tb.Provider, k)
			}
			for code, r := range tb.Regions {
				if _, ok := r.Instances[typ]; !ok {
					t.Fatalf("%s %s: default %q missing in %s", tb.Provider, k, typ, code)
				}
			}
		}
	}
	// archdoc.Defaults picks the AWS default type with matching specs.
	for _, k := range archdoc.NodeKinds() {
		n := archdoc.Defaults(k)
		if n.InstanceType != DefaultType(AWS, k) {
			t.Fatalf("%s: archdoc default %q, pricing default %q", k, n.InstanceType, DefaultType(AWS, k))
		}
		if n.InstanceType == "" {
			continue
		}
		it, _ := Lookup(AWS, "us-east-1", n.InstanceType)
		if n.CPUCores != it.VCPU || n.MemoryMiB != it.MemoryMiB || n.NICMbps != it.NICMbps {
			t.Fatalf("%s: defaults cpu/mem/nic %d/%d/%d, %s is %+v", k, n.CPUCores, n.MemoryMiB, n.NICMbps, n.InstanceType, it)
		}
	}
}

func TestRegionLatency(t *testing.T) {
	if RegionLatencyMicros("us-east-1", "us-east-1") != 0 {
		t.Fatal("same region must be 0")
	}
	ab, ba := RegionLatencyMicros("ap-northeast-2", "us-east-1"), RegionLatencyMicros("us-east-1", "ap-northeast-2")
	if ab != ba || ab <= RegionLatencyMicros("ap-northeast-2", "ap-northeast-1") {
		t.Fatalf("seoul-virginia %d / %d", ab, ba)
	}
	if RegionLatencyMicros("ap-northeast-2", "nowhere") != 150_000 || RegionLatencyMicros("", "us-east-1") != 150_000 {
		t.Fatal("unknown region must be 150ms")
	}
	if got := RegionLatencyMicros("ap-northeast-2", "koreacentral"); got <= 0 || got >= 10_000 {
		t.Fatalf("same metro across providers = %d", got)
	}
	metros := map[string]bool{}
	for _, m := range metro {
		metros[m] = true
	}
	for a := range metros {
		for b := range metros {
			k := [2]string{min(a, b), max(a, b)}
			if _, ok := metroRTT[k]; a != b && !ok {
				t.Errorf("no latency for %s–%s", a, b)
			}
		}
	}
	if HoursPerMonth != 730 {
		t.Fatal("HoursPerMonth")
	}
}

// TestNoNetworkImports checks every release GOOS, not only the host's: a
// _windows.go file deeper in the graph can pull in net (golang.org/x/sys/windows
// does) where the darwin and linux builds never see it.
func TestNoNetworkImports(t *testing.T) {
	for _, goos := range []string{"darwin", "linux", "windows"} {
		cmd := exec.Command("go", "list", "-deps", ".")
		cmd.Env = append(os.Environ(), "GOOS="+goos)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("GOOS=%s go list: %v\n%s", goos, err, out)
		}
		for _, dep := range strings.Fields(string(out)) {
			if dep == "net" || dep == "net/http" {
				t.Errorf("GOOS=%s: pricing depends on %s", goos, dep)
			}
		}
	}
}
