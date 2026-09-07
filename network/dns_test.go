package network

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestDNSOverrideRestoresSnapshot(t *testing.T) {
	var calls []string
	run := func(_ context.Context, name string, args ...string) (string, error) {
		call := name + " " + strings.Join(args, " ")
		calls = append(calls, call)
		if strings.Contains(call, "-getdnsservers") {
			return "9.9.9.9\n", nil
		}
		return "", nil
	}
	restore, err := applyDNS(t.Context(), "darwin", []string{"1.1.1.1"}, "Wi-Fi", run)
	if err != nil {
		t.Fatal(err)
	}
	if err := restore(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(calls, "\n"), "networksetup -setdnsservers Wi-Fi 9.9.9.9") {
		t.Fatalf("snapshot not restored: %v", calls)
	}
}

func TestDNSDoesNothingWithoutOverride(t *testing.T) {
	restore, err := applyDNS(
		t.Context(),
		"darwin",
		nil,
		"",
		func(context.Context, string, ...string) (string, error) {
			t.Fatal("unexpected network change")
			return "", nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := restore(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestDNSRejectsInvalidServerBeforeMutation(t *testing.T) {
	_, err := applyDNS(
		t.Context(),
		"darwin",
		[]string{"not-an-ip"},
		"Wi-Fi",
		func(context.Context, string, ...string) (string, error) {
			t.Fatal("invalid input reached OS")
			return "", nil
		},
	)
	if err == nil {
		t.Fatal("invalid DNS accepted")
	}
}

func TestFailedDNSChangeAttemptsRollback(t *testing.T) {
	calls := 0
	_, err := applyDNS(
		t.Context(),
		"darwin",
		[]string{"1.1.1.1"},
		"Wi-Fi",
		func(_ context.Context, _ string, args ...string) (string, error) {
			calls++
			if args[0] == "-getdnsservers" {
				return "9.9.9.9", nil
			}
			if args[len(args)-1] == "1.1.1.1" {
				return "", errors.New("failed change")
			}
			return "", nil
		},
	)
	if err == nil || calls != 3 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestFailedApplyPreservesRollbackFailure(t *testing.T) {
	_, err := applyDNS(
		t.Context(),
		"darwin",
		[]string{"1.1.1.1"},
		"Wi-Fi",
		func(_ context.Context, _ string, args ...string) (string, error) {
			if args[0] == "-getdnsservers" {
				return "9.9.9.9", nil
			}
			return "", errors.New("permission denied")
		},
	)
	if !errors.Is(err, ErrRestore) {
		t.Fatalf("rollback error not identifiable: %v", err)
	}
}

func TestPreparedDNSDiscoversBeforeApplying(t *testing.T) {
	for _, platform := range []string{"darwin", "linux"} {
		t.Run(platform, func(t *testing.T) {
			var calls []string
			run := func(_ context.Context, name string, args ...string) (string, error) {
				call := name + " " + strings.Join(args, " ")
				calls = append(calls, call)
				switch call {
				case "route -n get default":
					return "   interface: en7\n", nil
				case "networksetup -listnetworkserviceorder":
					return "(1) Office Ethernet\n(Hardware Port: USB LAN, Device: en7)\n", nil
				case "networksetup -getdnsservers Office Ethernet":
					return "9.9.9.9\n", nil
				case "ip -o route show default":
					return "default via 192.168.1.1 dev eth0 proto dhcp metric 100\n", nil
				case "resolvectl dns eth0":
					return "Link 2 (eth0): 9.9.9.9\n", nil
				}
				return "", nil
			}
			change, err := prepareDNS(t.Context(), platform, []string{"1.1.1.1"}, "", run)
			if err != nil {
				t.Fatal(err)
			}
			for _, call := range calls {
				if strings.Contains(call, "1.1.1.1") {
					t.Fatalf("prepare mutated DNS: %v", calls)
				}
			}
			if err := change.Apply(t.Context()); err != nil {
				t.Fatal(err)
			}
			if !strings.HasSuffix(calls[len(calls)-1], "1.1.1.1") {
				t.Fatalf("not applied: %v", calls)
			}
			if err := change.Restore(t.Context()); err != nil {
				t.Fatal(err)
			}
			if !strings.HasSuffix(calls[len(calls)-1], "9.9.9.9") {
				t.Fatalf("not restored: %v", calls)
			}
		})
	}
}

func TestDNSRejectsUnknownSnapshot(t *testing.T) {
	for _, platform := range []string{"darwin", "linux"} {
		_, err := prepareDNS(
			t.Context(),
			platform,
			[]string{"1.1.1.1"},
			"Office",
			func(_ context.Context, name string, args ...string) (string, error) {
				if name == "ip" {
					return "default via 192.168.1.1 dev eth0", nil
				}
				return "unexpected output", nil
			},
		)
		if err == nil {
			t.Fatalf("%s accepted unsafe snapshot", platform)
		}
	}
}

func TestMacDNSDiscoveryDoesNotGuess(t *testing.T) {
	for _, order := range []string{"(1) Wi-Fi\n(Hardware Port: Wi-Fi, Device: en0)", "(*) Disabled\n(Hardware Port: USB LAN, Device: en7)"} {
		_, err := prepareDNS(
			t.Context(),
			"darwin",
			[]string{"1.1.1.1"},
			"",
			func(_ context.Context, name string, args ...string) (string, error) {
				if name == "route" {
					return "interface: en7", nil
				}
				if args[0] != "-listnetworkserviceorder" {
					t.Fatalf("unexpected call: %s %v", name, args)
				}
				return order, nil
			},
		)
		if err == nil {
			t.Fatal("unmatched or disabled service accepted")
		}
	}
}

func TestRestoreBeforeApplyDoesNotMutateAndFailuresAreRetryable(t *testing.T) {
	changes := 0
	change, err := prepareDNS(
		t.Context(),
		"darwin",
		[]string{"1.1.1.1"},
		"Office",
		func(_ context.Context, _ string, args ...string) (string, error) {
			if args[0] == "-getdnsservers" {
				return "9.9.9.9", nil
			}
			changes++
			if changes == 2 {
				return "", errors.New("transient restoration failure")
			}
			return "", nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := change.Restore(t.Context()); err != nil || changes != 0 {
		t.Fatalf("restore before apply: changes=%d err=%v", changes, err)
	}
	if err := change.Apply(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := change.Restore(t.Context()); !errors.Is(err, ErrRestore) {
		t.Fatalf("missing restore error: %v", err)
	}
	if err := change.Restore(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := change.Restore(t.Context()); err != nil || changes != 3 {
		t.Fatalf("restore repeated: changes=%d err=%v", changes, err)
	}
}

func TestLinuxEmptyDNSSnapshotRestoresOnlyDNS(t *testing.T) {
	var mutations []string
	change, err := prepareDNS(
		t.Context(),
		"linux",
		[]string{"1.1.1.1"},
		"",
		func(_ context.Context, name string, args ...string) (string, error) {
			if name == "ip" {
				return "default via 192.168.1.1 dev eth0", nil
			}
			if len(args) == 2 {
				return "Link 2 (eth0):\n", nil
			}
			mutations = append(mutations, strings.Join(args, "|"))
			return "", nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := change.Apply(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := change.Restore(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(mutations) != 2 || mutations[1] != "dns|eth0|" {
		t.Fatalf("restore changed more than DNS: %v", mutations)
	}
}
