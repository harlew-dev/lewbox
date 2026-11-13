package machine_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/harlew-dev/lewbox/internal/aerolvm"
	"github.com/harlew-dev/lewbox/internal/aerolvm/aerolvmtest"
	"github.com/harlew-dev/lewbox/internal/machine"
	"github.com/harlew-dev/lewbox/internal/machine/machinetest"
)

func TestRegistryRoot(t *testing.T) {
	cases := map[string]string{
		"":          "/clusters/01abc",
		"lewbox":    "/lewbox/clusters/01abc",
		"/a/b/":     "/a/b/clusters/01abc",
		"team/prod": "/team/prod/clusters/01abc",
	}
	for prefix, want := range cases {
		if got := machine.RegistryRoot(prefix, "01ABC"); got != want {
			t.Errorf("RegistryRoot(%q) = %q, want %q", prefix, got, want)
		}
	}
}

func TestRegistryConfigQuotesEveryValue(t *testing.T) {
	cfg := machine.RegistryConfig(machine.RegistryStorage{
		Endpoint:        "https://acct.r2.cloudflarestorage.com",
		Region:          "auto",
		Bucket:          "snaps",
		RootDirectory:   "/clusters/x",
		PathStyle:       true,
		AccessKeyID:     "AKID",
		SecretAccessKey: "s3cr\"et\nbucket: evil",
	}, "httpsecret")
	for _, want := range []string{
		`    secretkey: "s3cr\"et\nbucket: evil"`,
		`    regionendpoint: "https://acct.r2.cloudflarestorage.com"`,
		`    forcepathstyle: true`,
		`    bucket: "snaps"`,
		`    chunksize: 104857600`,
		`  addr: "127.0.0.1:5000"`,
		`  secret: "httpsecret"`,
	} {
		if !strings.Contains(cfg, want+"\n") {
			t.Errorf("config is missing %q:\n%s", want, cfg)
		}
	}
	if strings.Count(cfg, "\n    bucket:") != 1 {
		t.Errorf("a secret broke out of its value:\n%s", cfg)
	}

	aws := machine.RegistryConfig(machine.RegistryStorage{Region: "eu-west-1", Bucket: "b", RootDirectory: "/clusters/x"}, "s")
	if strings.Contains(aws, "regionendpoint") || strings.Contains(aws, "chunksize") {
		t.Errorf("AWS config has an endpoint or an R2 chunk size:\n%s", aws)
	}
}

func TestPushEnv(t *testing.T) {
	env := machine.PushEnv("01ABC-DEF")
	for _, want := range []string{
		"SB_SNAPSHOT_PUSH_ENABLED=true",
		"SB_MIRROR_PUSH_HOST=127.0.0.1:5000",
		"SB_IMAGE_DISTRIBUTION_AOCR_HOST=127.0.0.1:5000",
		"SB_AUTO_IMPORT_CLUSTER_ID=01abc-def",
		"SB_AUTO_IMPORT_CLUSTER_PAT_PATH=" + machine.RegistryTokenFile,
	} {
		if !strings.Contains(env, want+"\n") {
			t.Errorf("PushEnv is missing %q:\n%s", want, env)
		}
	}
}

// registryArchive builds a release-shaped archive holding a fake binary.
func registryArchive(t *testing.T, binary []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range []struct {
		name string
		data []byte
	}{{"LICENSE", []byte("license")}, {"registry", binary}} {
		if err := tw.WriteHeader(&tar.Header{Name: f.name, Mode: 0o755, Size: int64(len(f.data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(f.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestRegistrySourceRefusesAnUnpinnedArchive(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "registry"), 0o755); err != nil {
		t.Fatal(err)
	}
	archive := registryArchive(t, []byte("not the real registry"))
	if err := os.WriteFile(filepath.Join(dir, "registry", machine.RegistryArchive("amd64")), archive, 0o644); err != nil {
		t.Fatal(err)
	}
	src := &machine.RegistrySource{From: dir, Arch: "amd64", CacheDir: t.TempDir()}
	if _, err := src.Binary(context.Background()); err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("Binary() error = %v, want a checksum mismatch", err)
	}
	if _, err := (&machine.RegistrySource{From: dir, Arch: "riscv64"}).Binary(context.Background()); err == nil {
		t.Fatal("an architecture with no pin was accepted")
	}
}

func TestApplyRegistryWritesConfigOnceAndMasksSecrets(t *testing.T) {
	runner := machinetest.New()
	files := map[string]string{}
	runner.OutputFn = func(_ context.Context, cmd machine.Cmd) ([]byte, error) {
		switch cmd.Name {
		case "cat":
			if v, ok := files[cmd.Args[0]]; ok {
				return []byte(v), nil
			}
			return nil, errors.New("no such file")
		case "systemctl":
			return []byte("active\n"), nil
		case "sha256sum":
			if v, ok := files[machine.RegistryBinary]; ok {
				sum := sha256.Sum256([]byte(v))
				return []byte(hex.EncodeToString(sum[:]) + "  " + machine.RegistryBinary + "\n"), nil
			}
			return nil, errors.New("no such file")
		case "id", "test":
			return nil, nil
		}
		return nil, nil
	}
	runner.RunFn = func(_ context.Context, cmd machine.Cmd) error {
		if cmd.Name == "install" && cmd.Stdin != nil {
			data := new(bytes.Buffer)
			if _, err := data.ReadFrom(cmd.Stdin); err != nil {
				return err
			}
			files[cmd.Args[len(cmd.Args)-1]] = data.String()
		}
		return nil
	}
	st := machine.RegistryStorage{Endpoint: "http://10.0.0.9:9000", Region: "us-east-1", Bucket: "b", RootDirectory: "/clusters/c", PathStyle: true, AccessKeyID: "AK", SecretAccessKey: "very-secret-key"}

	changed, err := machine.ApplyRegistry(context.Background(), runner, []byte("bin"), st, nil)
	if err != nil || !changed {
		t.Fatalf("first apply: changed=%v err=%v", changed, err)
	}
	if !runner.Ran("systemctl", "restart", "lewbox-registry") {
		t.Error("the registry was not restarted after its first install")
	}
	for _, line := range runner.Displays() {
		if strings.Contains(line, "very-secret-key") {
			t.Errorf("a command shows the secret: %s", line)
		}
	}
	if !strings.Contains(files[machine.RegistryConfigFile], `secretkey: "very-secret-key"`) {
		t.Errorf("config = %s", files[machine.RegistryConfigFile])
	}
	if files[machine.RegistryUnitFile] != machine.RegistryUnit {
		t.Error("unit not written")
	}
	// skopeo, which builds Firecracker disks, needs to be told the registry
	// is plain HTTP.
	if conf := files[machine.RegistriesConfFile]; !strings.Contains(conf, `location = "127.0.0.1:5000"`) || !strings.Contains(conf, "insecure = true") {
		t.Errorf("registries.conf drop-in = %q", conf)
	}

	runner.Reset()
	changed, err = machine.ApplyRegistry(context.Background(), runner, []byte("bin"), st, nil)
	if err != nil || changed {
		t.Fatalf("second apply: changed=%v err=%v, want no change (the http secret must be kept)", changed, err)
	}
	if runner.Ran("systemctl", "restart", "lewbox-registry") {
		t.Error("an unchanged registry was restarted")
	}

	// A node configured before the drop-in existed gets it, without a
	// registry restart.
	delete(files, machine.RegistriesConfFile)
	runner.Reset()
	changed, err = machine.ApplyRegistry(context.Background(), runner, []byte("bin"), st, nil)
	if err != nil || !changed {
		t.Fatalf("apply without the drop-in: changed=%v err=%v, want a change", changed, err)
	}
	if files[machine.RegistriesConfFile] != machine.RegistriesConf {
		t.Error("the drop-in was not written again")
	}
	if runner.Ran("systemctl", "restart", "lewbox-registry") {
		t.Error("the registry was restarted for the drop-in alone")
	}
}

func TestCleanupArchives(t *testing.T) {
	fake := aerolvmtest.New(t)
	fake.AdminToken = "avm_pat_over_loopback"
	ref := "127.0.0.1:5000/cluster/c1/snapshots/tenant-aa/archive-sb-1:latest"
	fake.AddSnapshot("t1", aerolvm.Snapshot{Name: "archive-sb-1", Image: "aerolvm-snapshot/aaa:latest", PushState: "active", ImageRegistryRef: ref})
	fake.AddSnapshot("t1", aerolvm.Snapshot{Name: "archive-sb-2", Image: "aerolvm-snapshot/bbb:latest", PushState: "pushing"})
	fake.AddSnapshot("t2", aerolvm.Snapshot{Name: "my-checkpoint", Image: "aerolvm-snapshot/ccc:latest", PushState: "active", ImageRegistryRef: "127.0.0.1:5000/x"})
	fake.AddSnapshot("t2", aerolvm.Snapshot{Name: "archive-sb-3", Image: "aerolvm-snapshot/ddd:latest", PushState: "active", ImageRegistryRef: "127.0.0.1:5000/cluster/c1/snapshots/tenant-bb/archive-sb-3:latest"})
	// An earlier pass removed this one's image; its record stays.
	fake.AddSnapshot("t2", aerolvm.Snapshot{Name: "archive-sb-4", Image: "aerolvm-snapshot/gone:latest", PushState: "active", ImageRegistryRef: "127.0.0.1:5000/cluster/c1/snapshots/tenant-bb/archive-sb-4:latest"})

	runner := machinetest.New()
	runner.OutputFn = func(_ context.Context, cmd machine.Cmd) ([]byte, error) {
		return []byte(strings.Join([]string{
			"ubuntu:22.04",
			"aerolvm-snapshot/aaa:latest",
			"aerolvm-snapshot/bbb:latest",
			"aerolvm-snapshot/ccc:latest",
			"aerolvm-snapshot/ddd:latest",
			ref,
			"127.0.0.1:5000/cluster/c1/snapshots/tenant-aa/my-checkpoint:latest",
			"127.0.0.1:5000/cluster/c1/snapshots/tenant-aa/archive-sb-9:latest",
			"aerolvm-snapshot/eee:latest",
		}, "\n")), nil
	}
	runner.RunFn = func(_ context.Context, cmd machine.Cmd) error {
		if cmd.Args[len(cmd.Args)-1] == "aerolvm-snapshot/ddd:latest" {
			return errors.New("image is being used by running container")
		}
		return nil
	}
	got, err := machine.CleanupArchives(context.Background(), runner, aerolvm.New(fake.URL(), fake.AdminToken), false, 0, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	wantRemoved := []string{"aerolvm-snapshot/aaa:latest", ref, "127.0.0.1:5000/cluster/c1/snapshots/tenant-aa/archive-sb-9:latest"}
	if strings.Join(sorted(got.Removed), ",") != strings.Join(sorted(wantRemoved), ",") {
		t.Errorf("removed = %v, want %v", got.Removed, wantRemoved)
	}
	if strings.Join(got.Kept, ",") != "aerolvm-snapshot/ddd:latest" {
		t.Errorf("kept = %v, want the image a container uses", got.Kept)
	}
	if runner.Ran("docker", "image", "rm", "aerolvm-snapshot/gone:latest") {
		t.Error("tried to remove an image that is no longer on the node")
	}
	for _, cmd := range runner.Commands() {
		if cmd.Sudo {
			t.Errorf("ran %s with sudo as root", cmd.Display())
		}
	}
}

func TestCleanupArchivesOlderThan(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	fake := aerolvmtest.New(t)
	fake.AdminToken = "avm_pat_over_loopback"
	ref := func(name string) string { return "127.0.0.1:5000/cluster/c1/snapshots/tenant-aa/" + name + ":latest" }
	fake.AddSnapshot("t1", aerolvm.Snapshot{Name: "archive-sb-old", Image: "aerolvm-snapshot/old:latest", PushState: "active",
		ImageRegistryRef: ref("archive-sb-old"), CreatedAt: now.Add(-25 * time.Hour)})
	fake.AddSnapshot("t1", aerolvm.Snapshot{Name: "archive-sb-new", Image: "aerolvm-snapshot/new:latest", PushState: "active",
		ImageRegistryRef: ref("archive-sb-new"), CreatedAt: now.Add(-time.Hour)})

	pulledOld, pulledNew, pulledUnknown := ref("archive-sb-7"), ref("archive-sb-8"), ref("archive-sb-9")
	runner := machinetest.New()
	runner.OutputFn = func(_ context.Context, cmd machine.Cmd) ([]byte, error) {
		if cmd.Args[0] == "images" {
			return []byte(strings.Join([]string{"aerolvm-snapshot/old:latest", "aerolvm-snapshot/new:latest", pulledOld, pulledNew, pulledUnknown}, "\n")), nil
		}
		// docker image inspect: the image was built long ago; the tag (the
		// pull) decides its age.
		switch cmd.Args[len(cmd.Args)-1] {
		case pulledOld:
			return []byte(`"2026-11-01T00:00:00Z" "2026-11-12T10:00:00.5Z"` + "\n"), nil
		case pulledNew:
			return []byte(`"2026-11-01T00:00:00Z" "2026-11-13T11:00:00Z"` + "\n"), nil
		}
		return nil, errors.New("no such image")
	}
	got, err := machine.CleanupArchives(context.Background(), runner, aerolvm.New(fake.URL(), fake.AdminToken), false, 24*time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"aerolvm-snapshot/old:latest", pulledOld}; strings.Join(sorted(got.Removed), ",") != strings.Join(sorted(want), ",") {
		t.Errorf("removed = %v, want %v", got.Removed, want)
	}
	// An image whose age can't be read is kept.
	if want := []string{"aerolvm-snapshot/new:latest", pulledNew, pulledUnknown}; strings.Join(sorted(got.Young), ",") != strings.Join(sorted(want), ",") {
		t.Errorf("young = %v, want %v", got.Young, want)
	}
}

func TestCleanupServiceUnit(t *testing.T) {
	if unit := machine.CleanupServiceUnit(0); !strings.Contains(unit, "ExecStart="+machine.CleanupBinary+" node cleanup --json\n") {
		t.Errorf("instant unit:\n%s", unit)
	}
	if unit := machine.CleanupServiceUnit(24 * time.Hour); !strings.Contains(unit, " node cleanup --json --older-than 24h0m0s\n") {
		t.Errorf("1d unit:\n%s", unit)
	}
}

func sorted(s []string) []string {
	out := slices.Clone(s)
	slices.Sort(out)
	return out
}
