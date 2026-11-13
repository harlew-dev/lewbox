package objstore_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/harlew-dev/lewbox/internal/objstore"
	"github.com/harlew-dev/lewbox/internal/objstore/objstoretest"
)

// newFixture returns a fake bucket and a client for it.
func newFixture(t *testing.T) (*objstoretest.Fake, *objstore.Client) {
	t.Helper()
	fake := objstoretest.New(t)
	fake.AddBucket("assets")
	c, err := objstore.New(objstore.Config{
		Endpoint:        fake.URL,
		Region:          "us-east-1",
		Bucket:          "assets",
		AccessKeyID:     fake.AccessKeyID,
		SecretAccessKey: fake.SecretAccessKey,
		PathStyle:       true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return fake, c
}

func TestConfigValidation(t *testing.T) {
	base := objstore.Config{Region: "us-east-1", Bucket: "b", AccessKeyID: "id", SecretAccessKey: "secret"}
	cases := []struct {
		name string
		edit func(*objstore.Config)
	}{
		{"no region", func(c *objstore.Config) { c.Region = "" }},
		{"no bucket", func(c *objstore.Config) { c.Bucket = "" }},
		{"no key", func(c *objstore.Config) { c.AccessKeyID = "" }},
		{"no secret", func(c *objstore.Config) { c.SecretAccessKey = "" }},
		{"bad scheme", func(c *objstore.Config) { c.Endpoint = "ftp://example.com" }},
		{"no host", func(c *objstore.Config) { c.Endpoint = "https://" }},
		{"with a path", func(c *objstore.Config) { c.Endpoint = "https://example.com/storage" }},
		{"with a query", func(c *objstore.Config) { c.Endpoint = "https://example.com?x=1" }},
		{"with user info", func(c *objstore.Config) { c.Endpoint = "https://user@example.com" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			tc.edit(&cfg)
			if _, err := objstore.New(cfg); err == nil {
				t.Fatal("New succeeded")
			}
		})
	}

	c, err := objstore.New(objstore.Config{Region: "auto", Bucket: "b", AccessKeyID: "id", SecretAccessKey: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if c == nil {
		t.Fatal("no client")
	}
}

func TestPutGetListDelete(t *testing.T) {
	fake, c := newFixture(t)
	ctx := context.Background()

	key := "clusters/c1/hello world+1.txt"
	if err := c.Put(ctx, key, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	body, err := c.Get(ctx, key)
	if err != nil || string(body) != "hello" {
		t.Fatalf("get = %q, %v", body, err)
	}
	if got := fake.Objects("assets")[key]; string(got) != "hello" {
		t.Errorf("stored = %q", got)
	}

	objs, next, err := c.List(ctx, "clusters/", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if next != "" || len(objs) != 1 || objs[0].Key != key || objs[0].Size != 5 || objs[0].LastModified.IsZero() {
		t.Fatalf("list = %+v, next %q, %v", objs, next, err)
	}

	if err := c.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Get(ctx, key); !errors.Is(err, objstore.ErrNotFound) {
		t.Fatalf("get after delete = %v", err)
	}
	if err := c.Delete(ctx, key); err != nil {
		t.Errorf("delete of a missing key = %v", err)
	}
}

func TestGetNotFound(t *testing.T) {
	_, c := newFixture(t)
	if _, err := c.Get(context.Background(), "missing"); !errors.Is(err, objstore.ErrNotFound) {
		t.Fatalf("get = %v", err)
	}
}

func TestPageThroughListing(t *testing.T) {
	fake, c := newFixture(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		key := fmt.Sprintf("page/%d", i)
		if err := c.Put(ctx, key, []byte(key)); err != nil {
			t.Fatal(err)
		}
	}
	var got []string
	next := ""
	for {
		objs, token, err := c.List(ctx, "page/", next, 1)
		if err != nil {
			t.Fatal(err)
		}
		for _, obj := range objs {
			got = append(got, obj.Key)
		}
		if token == "" {
			break
		}
		next = token
	}
	if strings.Join(got, ",") != "page/0,page/1,page/2" {
		t.Fatalf("keys = %v", got)
	}
	if len(fake.Objects("assets")) != 3 {
		t.Errorf("objects = %v", fake.Objects("assets"))
	}
}

func TestDeletePrefix(t *testing.T) {
	_, c := newFixture(t)
	ctx := context.Background()
	for i := 0; i < 1001; i++ {
		if err := c.Put(ctx, fmt.Sprintf("many/%04d", i), []byte("x")); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Put(ctx, "keep/me", []byte("x")); err != nil {
		t.Fatal(err)
	}
	removed, err := c.DeletePrefix(ctx, "many/")
	if err != nil || removed != 1001 {
		t.Fatalf("delete prefix = %d, %v", removed, err)
	}
	if _, err := c.Get(ctx, "many/0000"); !errors.Is(err, objstore.ErrNotFound) {
		t.Errorf("an object survived: %v", err)
	}
	if _, err := c.Get(ctx, "keep/me"); err != nil {
		t.Errorf("deleted outside the prefix: %v", err)
	}
}

func TestAPIError(t *testing.T) {
	fake, good := newFixture(t)
	c, err := objstore.New(objstore.Config{
		Endpoint:        fake.URL,
		Region:          "us-east-1",
		Bucket:          "assets",
		AccessKeyID:     "wrong",
		SecretAccessKey: fake.SecretAccessKey,
		PathStyle:       true,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = c.Put(context.Background(), "a", []byte("b"))
	apiErr, ok := errors.AsType[*objstore.APIError](err)
	if !ok || apiErr.Status != http.StatusForbidden || apiErr.Code != "InvalidAccessKeyId" {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(apiErr.Error(), fake.SecretAccessKey) {
		t.Errorf("the error carries the secret: %v", apiErr)
	}

	fake.FailNext(http.MethodGet, "boom", 500, "InternalError", 1)
	_, err = good.Get(context.Background(), "boom")
	if apiErr, ok := errors.AsType[*objstore.APIError](err); !ok || apiErr.Code != "InternalError" {
		t.Fatalf("error = %v", err)
	}
}

func TestUnknownBucket(t *testing.T) {
	fake, _ := newFixture(t)
	c, err := objstore.New(objstore.Config{
		Endpoint:        fake.URL,
		Region:          "us-east-1",
		Bucket:          "missing",
		AccessKeyID:     fake.AccessKeyID,
		SecretAccessKey: fake.SecretAccessKey,
		PathStyle:       true,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = c.Put(context.Background(), "a", []byte("b"))
	apiErr, ok := errors.AsType[*objstore.APIError](err)
	if !ok || apiErr.Code != "NoSuchBucket" {
		t.Fatalf("error = %v", err)
	}
}

func TestClockOption(t *testing.T) {
	fake, _ := newFixture(t)
	fixed := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	c, err := objstore.New(objstore.Config{
		Endpoint:        fake.URL,
		Region:          "us-east-1",
		Bucket:          "assets",
		AccessKeyID:     fake.AccessKeyID,
		SecretAccessKey: fake.SecretAccessKey,
		PathStyle:       true,
	}, objstore.WithClock(func() time.Time { return fixed }))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Put(context.Background(), "clock", []byte("x")); err != nil {
		t.Fatal(err)
	}
}
