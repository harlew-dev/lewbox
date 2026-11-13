package objstore

import (
	"net/http"
	"net/url"
	"testing"
	"time"
)

// TestSignAWSExample signs the "GET Object" example from AWS's SigV4
// documentation for S3, and compares the signature it publishes.
func TestSignAWSExample(t *testing.T) {
	c, err := New(Config{
		Region:          "us-east-1",
		Bucket:          "examplebucket",
		AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
		SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
	})
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse("https://examplebucket.s3.amazonaws.com/test.txt")
	if err != nil {
		t.Fatal(err)
	}
	headers := http.Header{"Range": {"bytes=0-9"}}
	c.sign(http.MethodGet, u, headers, emptyPayloadHash,
		time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC))

	const want = "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request, " +
		"SignedHeaders=host;range;x-amz-content-sha256;x-amz-date, " +
		"Signature=f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41"
	if got := headers.Get("Authorization"); got != want {
		t.Fatalf("Authorization = %q, want %q", got, want)
	}
	if got := headers.Get("x-amz-date"); got != "20130524T000000Z" {
		t.Errorf("x-amz-date = %q", got)
	}
}

// TestVirtualHostedURL checks the URL shape for AWS (no endpoint), where the
// bucket is the first host label.
func TestVirtualHostedURL(t *testing.T) {
	c, err := New(Config{
		Region:          "auto",
		Bucket:          "assets",
		AccessKeyID:     "id",
		SecretAccessKey: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.url("a/b.txt", nil)
	if err != nil {
		t.Fatal(err)
	}
	if u.Host != "assets.s3.auto.amazonaws.com" || u.Path != "/a/b.txt" {
		t.Fatalf("url = %s", u)
	}
	if u2, err := c.url("a b.txt", nil); err != nil || u2.EscapedPath() != "/a%20b.txt" {
		t.Fatalf("escaped url = %v, %v", u2, err)
	}
}
