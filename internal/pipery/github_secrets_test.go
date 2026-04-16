package pipery

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestLoadGitHubActionsSecretNames(t *testing.T) {
	requestsByPath := make(map[string]int)
	client := &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			requestsByPath[request.URL.Path]++

			if got := request.Header.Get("Authorization"); got != "Bearer test-token" {
				t.Fatalf("expected authorization header, got %q", got)
			}
			if got := request.Header.Get("X-GitHub-Api-Version"); got != githubAPIVersion {
				t.Fatalf("expected API version %q, got %q", githubAPIVersion, got)
			}
			if got := request.URL.Query().Get("page"); got != "1" {
				t.Fatalf("expected page 1, got %q", got)
			}

			switch request.URL.Path {
			case "/repos/pipery-dev/pipery/actions/secrets":
				return &http.Response{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(strings.NewReader(`{
						"total_count": 2,
						"secrets": [
							{"name": "REPO_SECRET"},
							{"name": "API_TOKEN"}
						]
					}`)),
					Header: make(http.Header),
				}, nil
			case "/repos/pipery-dev/pipery/actions/organization-secrets":
				return &http.Response{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(strings.NewReader(`{
						"total_count": 2,
						"secrets": [
							{"name": "ORG_SECRET"},
							{"name": "API_TOKEN"}
						]
					}`)),
					Header: make(http.Header),
				}, nil
			default:
				t.Fatalf("unexpected request path %q", request.URL.Path)
				return nil, nil
			}
		}),
	}

	names, err := loadGitHubActionsSecretNames([]string{
		"GITHUB_ACTIONS=true",
		"GITHUB_TOKEN=test-token",
		"GITHUB_REPOSITORY=pipery-dev/pipery",
		"GITHUB_API_URL=https://api.github.test",
	}, client)
	if err != nil {
		t.Fatalf("loadGitHubActionsSecretNames returned error: %v", err)
	}

	if requestsByPath["/repos/pipery-dev/pipery/actions/secrets"] != 1 {
		t.Fatalf("expected repository secrets endpoint to be called once, got %d", requestsByPath["/repos/pipery-dev/pipery/actions/secrets"])
	}
	if requestsByPath["/repos/pipery-dev/pipery/actions/organization-secrets"] != 1 {
		t.Fatalf("expected organization secrets endpoint to be called once, got %d", requestsByPath["/repos/pipery-dev/pipery/actions/organization-secrets"])
	}
	if len(names) != 3 {
		t.Fatalf("expected 3 unique secret names, got %#v", names)
	}
	if !names["REPO_SECRET"] || !names["API_TOKEN"] || !names["ORG_SECRET"] {
		t.Fatalf("unexpected secret names %#v", names)
	}
}

func TestBuildRedactionConfigAddsGitHubSecretNames(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			switch request.URL.Path {
			case "/repos/pipery-dev/pipery/actions/secrets":
				return &http.Response{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(strings.NewReader(`{
						"total_count": 1,
						"secrets": [
							{"name": "REPO_SECRET"}
						]
					}`)),
					Header: make(http.Header),
				}, nil
			case "/repos/pipery-dev/pipery/actions/organization-secrets":
				return &http.Response{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(strings.NewReader(`{
						"total_count": 1,
						"secrets": [
							{"name": "ORG_SECRET"}
						]
					}`)),
					Header: make(http.Header),
				}, nil
			default:
				t.Fatalf("unexpected request path %q", request.URL.Path)
				return nil, nil
			}
		}),
	}

	redaction := buildRedactionConfig(config{
		SecretNames:    []string{"USER_SECRET"},
		SecretPrefixes: []string{"ORG_"},
		SecretSuffixes: []string{"_TAIL"},
	}, []string{
		"GITHUB_ACTIONS=true",
		"GITHUB_TOKEN=test-token",
		"GITHUB_REPOSITORY=pipery-dev/pipery",
		"GITHUB_API_URL=https://api.github.test",
	}, client)

	expectedSecretNames := []string{"ORG_SECRET", "REPO_SECRET", "USER_SECRET"}
	if len(redaction.SecretNames) != len(expectedSecretNames) {
		t.Fatalf("expected secret names %#v, got %#v", expectedSecretNames, redaction.SecretNames)
	}
	for index, expectedName := range expectedSecretNames {
		if redaction.SecretNames[index] != expectedName {
			t.Fatalf("expected secret names %#v, got %#v", expectedSecretNames, redaction.SecretNames)
		}
	}
	if len(redaction.SecretPrefixes) != 1 || redaction.SecretPrefixes[0] != "ORG_" {
		t.Fatalf("expected prefixes to pass through, got %#v", redaction.SecretPrefixes)
	}
	if len(redaction.SecretSuffixes) != 1 || redaction.SecretSuffixes[0] != "_TAIL" {
		t.Fatalf("expected suffixes to pass through, got %#v", redaction.SecretSuffixes)
	}
}
