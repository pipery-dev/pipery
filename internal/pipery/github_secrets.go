package pipery

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	neturl "net/url"
	"slices"
	"strings"
)

const githubAPIVersion = "2026-03-10"

type githubRepositorySecretsResponse struct {
	TotalCount int `json:"total_count"`
	Secrets    []struct {
		Name string `json:"name"`
	} `json:"secrets"`
}

// buildRedactionConfig merges user-provided redaction rules with any secret
// names that can be discovered automatically from the GitHub Actions runtime.
func buildRedactionConfig(cfg config, env []string, client *http.Client) redactionConfig {
	secretNamesMap := make(map[string]bool, 0)
	appendUniqueStrings(secretNamesMap, cfg.SecretNames...)
	githubSecretNames, err := loadGitHubActionsSecretNames(env, client)
	if err == nil {
		maps.Copy(secretNamesMap, githubSecretNames)
	}

	return redactionConfig{
		SecretNames:    getMapKeys(secretNamesMap),
		SecretPrefixes: cfg.SecretPrefixes,
		SecretSuffixes: cfg.SecretSuffixes,
	}
}

// loadGitHubActionsSecretNames asks the GitHub REST API for the names of
// repository-level secrets and organization secrets shared with the workflow
// repository.
//
// Important limitation: GitHub does not expose secret values through this API.
// We use the returned names to find matching environment variables in the
// current process, and then scrub those values from the logged fields.
func loadGitHubActionsSecretNames(env []string, client *http.Client) (map[string]bool, error) {
	envMap := envSliceToMap(env)
	if !strings.EqualFold(envMap["GITHUB_ACTIONS"], "true") {
		return nil, nil
	}

	token := envMap["GITHUB_TOKEN"]
	repository := envMap["GITHUB_REPOSITORY"]
	if token == "" || repository == "" {
		return nil, nil
	}

	owner, repo, ok := strings.Cut(repository, "/")
	if !ok || owner == "" || repo == "" {
		return nil, nil
	}

	baseURL := envMap["GITHUB_API_URL"]
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}

	if client == nil {
		client = http.DefaultClient
	}

	endpoints := []string{
		fmt.Sprintf(
			"%s/repos/%s/%s/actions/secrets",
			strings.TrimRight(baseURL, "/"),
			neturl.PathEscape(owner),
			neturl.PathEscape(repo),
		),
		fmt.Sprintf(
			"%s/repos/%s/%s/actions/organization-secrets",
			strings.TrimRight(baseURL, "/"),
			neturl.PathEscape(owner),
			neturl.PathEscape(repo),
		),
	}

	collected := make(map[string]bool, 0)
	for _, endpoint := range endpoints {
		names, err := loadGitHubSecretNamesFromEndpoint(endpoint, token, client)
		if err != nil {
			return nil, err
		}
		appendUniqueStrings(collected, names...)
	}

	return collected, nil
}

func loadGitHubSecretNamesFromEndpoint(endpoint string, token string, client *http.Client) ([]string, error) {
	collected := make(map[string]bool, 0)
	page := 1

	for {
		request, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s?per_page=100&page=%d", endpoint, page), nil)
		if err != nil {
			return nil, err
		}

		request.Header.Set("Accept", "application/vnd.github+json")
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("X-GitHub-Api-Version", githubAPIVersion)

		response, err := client.Do(request)
		if err != nil {
			return nil, err
		}

		var payload githubRepositorySecretsResponse
		func() {
			defer response.Body.Close()

			if response.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
				err = fmt.Errorf("github secrets api returned %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
				return
			}

			err = json.NewDecoder(response.Body).Decode(&payload)
		}()
		if err != nil {
			return nil, err
		}

		if len(payload.Secrets) == 0 {
			break
		}

		for _, secret := range payload.Secrets {
			if secret.Name == "" {
				continue
			}
			appendUniqueStrings(collected, secret.Name)
		}

		if len(collected) >= payload.TotalCount || len(payload.Secrets) < 100 {
			break
		}
		page++
	}

	return getMapKeys(collected), nil
}

func appendUniqueStrings(values map[string]bool, extras ...string) {
	for _, extra := range extras {
		if extra == "" || values[extra] {
			continue
		}
		values[extra] = true
	}
}

func getMapKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
