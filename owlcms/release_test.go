package owlcms

import (
	"reflect"
	"testing"
)

func TestCatalogNeedsPrereleases(t *testing.T) {
	if !catalogNeedsPrereleases(true, nil) {
		t.Fatalf("expected explicit prerelease request to require prerelease catalog")
	}
	if !catalogNeedsPrereleases(false, []string{"3.3.0-rc03"}) {
		t.Fatalf("expected installed prerelease to require prerelease catalog")
	}
	if catalogNeedsPrereleases(false, []string{"3.2.0"}) {
		t.Fatalf("did not expect stable-only installs to require prerelease catalog")
	}
}

func TestFetchReleasesForCatalogIncludesPrereleasesWhenRequested(t *testing.T) {
	originalFetcher := fetchReleasesFromURL
	defer func() { fetchReleasesFromURL = originalFetcher }()

	var called []string
	fetchReleasesFromURL = func(url string) ([]Release, error) {
		called = append(called, url)
		switch url {
		case "https://api.github.com/repos/owlcms/releases/releases":
			return []Release{{TagName: "3.2.0"}}, nil
		case "https://api.github.com/repos/owlcms/prereleases/releases":
			return []Release{{TagName: "3.3.0-rc03"}}, nil
		default:
			t.Fatalf("unexpected URL: %s", url)
			return nil, nil
		}
	}

	releases, err := fetchReleasesForCatalog(true)
	if err != nil {
		t.Fatalf("fetchReleasesForCatalog(true): %v", err)
	}
	if !reflect.DeepEqual(called, []string{
			"https://api.github.com/repos/owlcms/releases/releases",
			"https://api.github.com/repos/owlcms/prereleases/releases",
	}) {
		t.Fatalf("unexpected fetch sequence: %v", called)
	}
	if !reflect.DeepEqual(releases, []string{"3.3.0-rc03", "3.2.0"}) {
		t.Fatalf("unexpected releases: %v", releases)
	}
}
