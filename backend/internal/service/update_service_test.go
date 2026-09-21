//go:build unit

package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type updateServiceCacheStub struct {
	data string
}

func (s *updateServiceCacheStub) GetUpdateInfo(context.Context) (string, error) {
	if s.data == "" {
		return "", errors.New("cache miss")
	}
	return s.data, nil
}

func (s *updateServiceCacheStub) SetUpdateInfo(_ context.Context, data string, _ time.Duration) error {
	s.data = data
	return nil
}

type updateServiceGitHubClientStub struct {
	release        *GitHubRelease
	recentReleases []*GitHubRelease
	recentErr      error
}

type branchUpdateClientStub struct {
	updateServiceGitHubClientStub
	commit     *BranchCommit
	published  bool
	publishErr error
}

func (s *branchUpdateClientStub) FetchBranchCommit(context.Context, string, string) (*BranchCommit, error) {
	return s.commit, nil
}

func (s *branchUpdateClientStub) HasPublishedCustomImage(context.Context, string, string, string) (bool, error) {
	return s.published, s.publishErr
}

func TestUpdateServiceDockerCheckUsesCommitSHA(t *testing.T) {
	t.Setenv("UPDATE_MODE", "docker")
	t.Setenv("UPDATE_DOCKER_STAGE_FILE", t.TempDir()+"/update-staged")
	commit := &BranchCommit{SHA: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", HTMLURL: "https://github.com/yunyar/sub2api/commit/bbbbbbbb"}
	commit.Commit.Message = "custom update"
	commit.Commit.Author.Date = "2026-09-21T00:00:00Z"
	svc := NewUpdateServiceWithCommit(
		&updateServiceCacheStub{},
		&branchUpdateClientStub{commit: commit, published: true},
		"0.1.0",
		"release",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	)

	info, err := svc.CheckUpdate(context.Background(), true)

	require.NoError(t, err)
	require.True(t, info.HasUpdate)
	require.Equal(t, commit.SHA, info.LatestCommit)
	require.Equal(t, "bbbbbbbbbbbb", info.LatestVersion)
	require.Equal(t, "docker", info.UpdateMode)
	require.Equal(t, "custom/community-qrcode", info.Branch)
	require.False(t, info.Staged)
}

func TestUpdateServiceDockerCheckRejectsUnpublishedBranchHead(t *testing.T) {
	t.Setenv("UPDATE_MODE", "docker")
	commit := &BranchCommit{SHA: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
	svc := NewUpdateServiceWithCommit(&updateServiceCacheStub{}, &branchUpdateClientStub{commit: commit}, "0.1.0", "release", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

	info, err := svc.CheckUpdate(context.Background(), true)

	require.NoError(t, err)
	require.False(t, info.HasUpdate)
	require.Empty(t, info.LatestCommit)
	require.Contains(t, info.Warning, "not published")
}

func TestUpdateServiceDockerCheckReportsExistingStageBeforePublicationGate(t *testing.T) {
	t.Setenv("UPDATE_MODE", "docker")
	stageFile := t.TempDir() + "/update-staged"
	t.Setenv("UPDATE_DOCKER_STAGE_FILE", stageFile)
	stagedCommit := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	require.NoError(t, os.WriteFile(stageFile, []byte("ghcr.io/yunyar/sub2api:custom-"+stagedCommit+"\n"), 0600))
	svc := NewUpdateServiceWithCommit(
		&updateServiceCacheStub{},
		&branchUpdateClientStub{commit: &BranchCommit{SHA: "invalid"}},
		"0.1.0",
		"release",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	)

	info, err := svc.CheckUpdate(context.Background(), true)

	require.NoError(t, err)
	require.True(t, info.Staged)
	require.Contains(t, info.Warning, "invalid commit SHA")
}

func TestUpdateServiceDockerStageClearsAfterBootingStagedCommit(t *testing.T) {
	t.Setenv("UPDATE_MODE", "docker")
	stageFile := t.TempDir() + "/update-staged"
	t.Setenv("UPDATE_DOCKER_STAGE_FILE", stageFile)
	commit := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	require.NoError(t, os.WriteFile(stageFile, []byte("ghcr.io/yunyar/sub2api:custom-"+commit+"\n"), 0600))
	svc := NewUpdateServiceWithCommit(&updateServiceCacheStub{}, &branchUpdateClientStub{}, "0.1.0", "release", commit)

	_, staged, err := svc.dockerStagedImage()

	require.NoError(t, err)
	require.False(t, staged)
	_, err = os.Stat(stageFile)
	require.True(t, os.IsNotExist(err))
}

func TestUpdateServiceDockerHostStageReadErrorDoesNotFallBackToAppMarker(t *testing.T) {
	t.Setenv("UPDATE_MODE", "docker")
	stageFile := t.TempDir() + "/update-staged"
	t.Setenv("UPDATE_DOCKER_STAGE_FILE", stageFile)
	t.Setenv("UPDATE_DOCKER_HOST_STATE_DIR", stageFile)
	commit := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	require.NoError(t, os.WriteFile(stageFile, []byte("ghcr.io/yunyar/sub2api:custom-"+commit), 0600))
	svc := NewUpdateServiceWithCommit(
		&updateServiceCacheStub{},
		&branchUpdateClientStub{commit: &BranchCommit{SHA: "invalid"}},
		"0.1.0",
		"release",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	)

	info, err := svc.CheckUpdate(context.Background(), true)

	require.NoError(t, err)
	require.False(t, info.Staged)
	require.Contains(t, info.Warning, "staged Docker state unavailable")
}

func TestUpdateServiceDockerCacheDoesNotReuseReleaseCache(t *testing.T) {
	t.Setenv("UPDATE_MODE", "")
	cache := &updateServiceCacheStub{}
	releaseSvc := NewUpdateService(cache, &updateServiceGitHubClientStub{}, "0.1.0", "release")
	releaseSvc.saveToCache(context.Background(), &UpdateInfo{LatestVersion: "9.9.9"})
	t.Setenv("UPDATE_MODE", "docker")
	dockerSvc := NewUpdateServiceWithCommit(cache, &branchUpdateClientStub{commit: &BranchCommit{SHA: "invalid"}}, "0.1.0", "release", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

	info, err := dockerSvc.CheckUpdate(context.Background(), false)

	require.NoError(t, err)
	require.False(t, info.Cached)
	require.Contains(t, info.Warning, "invalid commit SHA")
}

func TestUpdateServiceDockerDisablesBinaryRollback(t *testing.T) {
	t.Setenv("UPDATE_MODE", "docker")
	svc := NewUpdateService(&updateServiceCacheStub{}, &updateServiceGitHubClientStub{}, "0.1.0", "release")

	_, err := svc.ListRollbackVersions(context.Background())
	require.ErrorIs(t, err, ErrDockerRollbackNotSupported)
	require.ErrorIs(t, svc.RollbackToVersion(context.Background(), "0.0.9"), ErrDockerRollbackNotSupported)
	require.ErrorIs(t, svc.Rollback(), ErrDockerRollbackNotSupported)
}

func TestUpdateServiceDockerDownloadPullsAndStagesWithHostPaths(t *testing.T) {
	t.Setenv("UPDATE_MODE", "docker")
	t.Setenv("UPDATE_DOCKER_STAGE_FILE", t.TempDir()+"/update-staged")
	t.Setenv("UPDATE_DOCKER_HOST_DEPLOY_DIR", "/srv/sub2api")
	t.Setenv("UPDATE_DOCKER_COMPOSE_FILE", "/srv/sub2api/docker-compose.yml")
	t.Setenv("UPDATE_DOCKER_COMPOSE_OVERLAY_FILE", "/srv/sub2api/docker-compose.custom-updater.yml")
	t.Setenv("UPDATE_DOCKER_COMPOSE_PROJECT_NAME", "sub2api-prod")
	t.Setenv("UPDATE_DOCKER_HEALTH_TIMEOUT_SECONDS", "45")
	commit := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	svc := NewUpdateServiceWithCommit(&updateServiceCacheStub{}, &branchUpdateClientStub{commit: &BranchCommit{SHA: commit}, published: true}, "0.1.0", "release", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	var calls [][]string
	originalRunner := dockerCommandRunner
	dockerCommandRunner = func(_ context.Context, args ...string) error {
		require.NoError(t, validateDockerCommandArgs(args))
		calls = append(calls, append([]string(nil), args...))
		return nil
	}
	t.Cleanup(func() { dockerCommandRunner = originalRunner })

	require.NoError(t, svc.PerformUpdate(context.Background()))

	require.Len(t, calls, 2)
	require.Equal(t, []string{"pull", "ghcr.io/yunyar/sub2api:custom-" + commit}, calls[0])
	require.Equal(t, "run", calls[1][0])
	require.Contains(t, calls[1], "/srv/sub2api:/srv/sub2api")
	require.Contains(t, calls[1], "UPDATE_DOCKER_COMPOSE_OVERLAY_FILE=/srv/sub2api/docker-compose.custom-updater.yml")
	require.Contains(t, calls[1], "UPDATE_DOCKER_COMPOSE_PROJECT_NAME=sub2api-prod")
	require.Contains(t, calls[1], "UPDATE_DOCKER_HEALTH_TIMEOUT_SECONDS=45")
	require.Equal(t, []string{"stage", "ghcr.io/yunyar/sub2api:custom-" + commit, "/srv/sub2api/docker-compose.yml", "sub2api"}, calls[1][len(calls[1])-4:])
}

func TestUpdateServiceDockerRestartStartsOnlyHelperWithHostPaths(t *testing.T) {
	t.Setenv("UPDATE_MODE", "docker")
	stageFile := t.TempDir() + "/update-staged"
	t.Setenv("UPDATE_DOCKER_STAGE_FILE", stageFile)
	t.Setenv("UPDATE_DOCKER_HOST_DEPLOY_DIR", "/srv/sub2api")
	t.Setenv("UPDATE_DOCKER_COMPOSE_FILE", "/srv/sub2api/docker-compose.yml")
	t.Setenv("UPDATE_DOCKER_COMPOSE_OVERLAY_FILE", "/srv/sub2api/docker-compose.custom-updater.yml")
	commit := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	require.NoError(t, os.WriteFile(stageFile, []byte("ghcr.io/yunyar/sub2api:custom-"+commit), 0600))
	svc := NewUpdateServiceWithCommit(&updateServiceCacheStub{}, &branchUpdateClientStub{}, "0.1.0", "release", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	var calls [][]string
	originalRunner := dockerCommandRunner
	dockerCommandRunner = func(_ context.Context, args ...string) error {
		require.NoError(t, validateDockerCommandArgs(args))
		calls = append(calls, append([]string(nil), args...))
		return nil
	}
	t.Cleanup(func() { dockerCommandRunner = originalRunner })

	require.NoError(t, svc.RestartDocker(context.Background()))

	require.Len(t, calls, 1)
	require.Equal(t, "run", calls[0][0])
	require.Contains(t, calls[0], "-d")
	require.Contains(t, calls[0], "/srv/sub2api:/srv/sub2api")
	require.Contains(t, calls[0], "UPDATE_DOCKER_COMPOSE_OVERLAY_FILE=/srv/sub2api/docker-compose.custom-updater.yml")
	require.Equal(t, []string{"activate", "ghcr.io/yunyar/sub2api:custom-" + commit, "/srv/sub2api/docker-compose.yml", "sub2api"}, calls[0][len(calls[0])-4:])
}

func TestUpdateServiceDockerRestartPropagatesCLIError(t *testing.T) {
	t.Setenv("UPDATE_MODE", "docker")
	stageFile := t.TempDir() + "/update-staged"
	t.Setenv("UPDATE_DOCKER_STAGE_FILE", stageFile)
	commit := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	require.NoError(t, os.WriteFile(stageFile, []byte("ghcr.io/yunyar/sub2api:custom-"+commit), 0600))
	svc := NewUpdateServiceWithCommit(&updateServiceCacheStub{}, &branchUpdateClientStub{}, "0.1.0", "release", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	originalRunner := dockerCommandRunner
	dockerCommandRunner = func(context.Context, ...string) error { return errors.New("docker daemon unavailable") }
	t.Cleanup(func() { dockerCommandRunner = originalRunner })

	err := svc.RestartDocker(context.Background())

	require.Error(t, err)
	require.Contains(t, err.Error(), "start update helper")
	require.Contains(t, err.Error(), "docker daemon unavailable")
}

func TestUpdateServiceDockerStateContainment(t *testing.T) {
	directory := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	require.NoError(t, os.WriteFile(outside, []byte("unchanged"), 0600))
	link := filepath.Join(directory, "update-staged")
	require.NoError(t, os.Symlink(outside, link))
	_, err := readDockerState(link)
	require.Error(t, err)
	require.Error(t, writeDockerState(link, []byte("overwrite")))
	data, err := os.ReadFile(outside)
	require.NoError(t, err)
	require.Equal(t, "unchanged", string(data))
	for _, path := range []string{"relative/state", directory + "/../outside", "/state", directory + "/state\n"} {
		_, err := readDockerState(path)
		require.Error(t, err, path)
		require.Error(t, writeDockerState(path, nil), path)
	}
	regular := filepath.Join(directory, "regular")
	require.NoError(t, writeDockerState(regular, []byte("stage")))
	data, err = readDockerState(regular)
	require.NoError(t, err)
	require.Equal(t, "stage", string(data))
}

func TestUpdateServiceDockerRejectsUnsafeConfiguration(t *testing.T) {
	for _, test := range []struct{ key, value string }{
		{"UPDATE_DOCKER_HOST_DEPLOY_DIR", "/"},
		{"UPDATE_DOCKER_HOST_DEPLOY_DIR", "/srv/app:rw"},
		{"UPDATE_DOCKER_COMPOSE_FILE", "/etc/compose.yml"},
		{"UPDATE_DOCKER_COMPOSE_OVERLAY_FILE", "/srv/app/../other.yml"},
		{"UPDATE_DOCKER_STAGE_FILE", "../stage"},
		{"UPDATE_DOCKER_COMPOSE_SERVICE", "--privileged"},
		{"UPDATE_DOCKER_COMPOSE_PROJECT_NAME", "project;echo"},
		{"UPDATE_DOCKER_HEALTH_TIMEOUT_SECONDS", "0"},
		{"UPDATE_DOCKER_HEALTH_TIMEOUT_SECONDS", "3601"},
	} {
		t.Run(test.key+test.value, func(t *testing.T) {
			t.Setenv("UPDATE_DOCKER_HOST_DEPLOY_DIR", "/srv/app")
			t.Setenv("UPDATE_DOCKER_COMPOSE_FILE", "/srv/app/compose.yml")
			t.Setenv(test.key, test.value)
			require.Error(t, validateDockerUpdatePaths())
		})
	}
}

func TestUpdateServiceDockerCommandAllowlist(t *testing.T) {
	t.Setenv("UPDATE_DOCKER_IMAGE", "ghcr.io/yunyar/sub2api")
	image := "ghcr.io/yunyar/sub2api:custom-" + strings.Repeat("a", 40)
	require.NoError(t, validateDockerCommandArgs([]string{"pull", image}))
	for _, args := range [][]string{
		{"pull", "--help"},
		{"pull", "ghcr.io/other/image:custom-" + strings.Repeat("a", 40)},
		{"pull", "ghcr.io/yunyar/sub2api:latest"},
		{"pull", image, "--all-tags"},
		{"run", "--privileged", image},
		{"compose", "up", "-d"},
	} {
		require.Error(t, validateDockerCommandArgs(args), args)
	}
	t.Setenv("UPDATE_DOCKER_IMAGE", "--config=/tmp/evil")
	require.False(t, isCustomImageReference("--config=/tmp/evil:custom-"+strings.Repeat("a", 40)))
}

func (s *updateServiceGitHubClientStub) FetchLatestRelease(context.Context, string) (*GitHubRelease, error) {
	return s.release, nil
}

func (s *updateServiceGitHubClientStub) FetchRecentReleases(context.Context, string, int) ([]*GitHubRelease, error) {
	return s.recentReleases, s.recentErr
}

func (s *updateServiceGitHubClientStub) DownloadFile(context.Context, string, string, int64) error {
	panic("DownloadFile should not be called when no update is available")
}

func (s *updateServiceGitHubClientStub) FetchChecksumFile(context.Context, string) ([]byte, error) {
	panic("FetchChecksumFile should not be called when no update is available")
}

func TestUpdateServicePerformUpdateNoUpdateReturnsSentinel(t *testing.T) {
	svc := NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{
			release: &GitHubRelease{
				TagName: "v0.1.132",
				Name:    "v0.1.132",
			},
		},
		"0.1.132",
		"release",
	)

	err := svc.PerformUpdate(context.Background())

	require.Error(t, err)
	require.True(t, errors.Is(err, ErrNoUpdateAvailable))
	require.ErrorIs(t, err, ErrNoUpdateAvailable)
}

func newRollbackTestService(current string, releases []*GitHubRelease) *UpdateService {
	return NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{recentReleases: releases},
		current,
		"release",
	)
}

func TestUpdateServiceListRollbackVersionsFiltersAndCaps(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.148", PublishedAt: "2026-07-09T00:00:00Z"},                       // newer than current: excluded
		{TagName: "v0.1.147", PublishedAt: "2026-07-08T00:00:00Z"},                       // current: excluded
		{TagName: "v0.1.146-rc1", PublishedAt: "2026-07-07T12:00:00Z", Prerelease: true}, // prerelease: excluded
		{TagName: "v0.1.146", PublishedAt: "2026-07-07T00:00:00Z"},
		{TagName: "v0.1.145", PublishedAt: "2026-07-06T00:00:00Z", Draft: true}, // draft: excluded
		{TagName: "v0.1.144", PublishedAt: "2026-07-05T00:00:00Z"},
		{TagName: "v0.1.144", PublishedAt: "2026-07-05T00:00:00Z"}, // duplicate: excluded
		{TagName: "v0.1.143", PublishedAt: "2026-07-04T00:00:00Z"},
		{TagName: "v0.1.142", PublishedAt: "2026-07-03T00:00:00Z"}, // beyond cap of 3: excluded
	}
	svc := newRollbackTestService("0.1.147", releases)

	versions, err := svc.ListRollbackVersions(context.Background())

	require.NoError(t, err)
	require.Len(t, versions, 3)
	require.Equal(t, "0.1.146", versions[0].Version)
	require.Equal(t, "0.1.144", versions[1].Version)
	require.Equal(t, "0.1.143", versions[2].Version)
}

func TestUpdateServiceListRollbackVersionsSortsUnorderedInput(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.144"},
		{TagName: "v0.1.146"},
		{TagName: "v0.1.145"},
	}
	svc := newRollbackTestService("0.1.147", releases)

	versions, err := svc.ListRollbackVersions(context.Background())

	require.NoError(t, err)
	require.Len(t, versions, 3)
	require.Equal(t, "0.1.146", versions[0].Version)
	require.Equal(t, "0.1.145", versions[1].Version)
	require.Equal(t, "0.1.144", versions[2].Version)
}

func TestUpdateServiceListRollbackVersionsEmptyWhenNoneOlder(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.147"},
		{TagName: "v0.1.148"},
	}
	svc := newRollbackTestService("0.1.147", releases)

	versions, err := svc.ListRollbackVersions(context.Background())

	require.NoError(t, err)
	require.Empty(t, versions)
}

func TestUpdateServiceListRollbackVersionsPropagatesFetchError(t *testing.T) {
	svc := NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{recentErr: errors.New("github unavailable")},
		"0.1.147",
		"release",
	)

	_, err := svc.ListRollbackVersions(context.Background())

	require.Error(t, err)
	require.Contains(t, err.Error(), "github unavailable")
}

func TestUpdateServiceRollbackToVersionRejectsDisallowedTargets(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.148"},
		{TagName: "v0.1.147"},
		{TagName: "v0.1.146"},
		{TagName: "v0.1.145"},
		{TagName: "v0.1.144"},
		{TagName: "v0.1.143"},
		{TagName: "v0.1.142"},
	}
	svc := newRollbackTestService("0.1.147", releases)

	for _, target := range []string{
		"",         // empty
		"0.1.147",  // current version
		"v0.1.147", // current version with prefix
		"0.1.148",  // newer than current
		"0.1.142",  // older than the 3 most recent
		"9.9.9",    // nonexistent
	} {
		err := svc.RollbackToVersion(context.Background(), target)
		require.ErrorIs(t, err, ErrRollbackVersionNotAllowed, "target %q should be rejected", target)
	}
}

func TestUpdateServiceRollbackToVersionAcceptsVPrefix(t *testing.T) {
	// No platform asset in the release: the target passes the allowlist check
	// and fails later at asset lookup, proving the version itself was accepted.
	releases := []*GitHubRelease{
		{TagName: "v0.1.147"},
		{TagName: "v0.1.146"},
	}
	svc := newRollbackTestService("0.1.147", releases)

	err := svc.RollbackToVersion(context.Background(), "v0.1.146")

	require.Error(t, err)
	require.NotErrorIs(t, err, ErrRollbackVersionNotAllowed)
	require.Contains(t, err.Error(), "no compatible release found")
}
