package service

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrNoUpdateAvailable         = infraerrors.Conflict("ALREADY_UP_TO_DATE", "no update available; current version is latest")
	ErrRollbackVersionNotAllowed = infraerrors.BadRequest("ROLLBACK_VERSION_NOT_ALLOWED", "version is not in the allowed rollback list")
)

const (
	updateCacheKey = "update_check_cache"
	updateCacheTTL = 1200 // 20 minutes
	githubRepo     = "Wei-Shaw/sub2api"

	// Security: allowed download domains for updates
	allowedDownloadHost = "github.com"
	allowedAssetHost    = "objects.githubusercontent.com"

	// Security: max download size (500MB)
	maxDownloadSize = 500 * 1024 * 1024

	// Rollback: expose at most the 3 most recent versions older than current
	maxRollbackVersions = 3
	// Fetch a few extra releases so filtering (current/newer/prerelease) still leaves enough candidates
	rollbackFetchPageSize = 15
)

// UpdateCache defines cache operations for update service
type UpdateCache interface {
	GetUpdateInfo(ctx context.Context) (string, error)
	SetUpdateInfo(ctx context.Context, data string, ttl time.Duration) error
}

// GitHubReleaseClient 获取 GitHub release 信息的接口
type GitHubReleaseClient interface {
	FetchLatestRelease(ctx context.Context, repo string) (*GitHubRelease, error)
	FetchRecentReleases(ctx context.Context, repo string, perPage int) ([]*GitHubRelease, error)
	DownloadFile(ctx context.Context, url, dest string, maxSize int64) error
	FetchChecksumFile(ctx context.Context, url string) ([]byte, error)
}

// CustomBranchClient is implemented by the GitHub client when branch-based
// container updates are enabled. It is optional so source/release builds keep
// the existing release update behavior and test doubles remain compatible.
type CustomBranchClient interface {
	FetchBranchCommit(ctx context.Context, repo, branch string) (*BranchCommit, error)
}

// PublishedCustomImageClient verifies that the custom image workflow completed
// for a commit. A branch tip alone is not installable while its image is still
// building or if the publication job failed.
type PublishedCustomImageClient interface {
	HasPublishedCustomImage(ctx context.Context, repo, branch, commit string) (bool, error)
}

// UpdateService handles software updates
type UpdateService struct {
	cache          UpdateCache
	githubClient   GitHubReleaseClient
	currentVersion string
	buildType      string // "source" for manual builds, "release" for CI builds
	currentCommit  string
	updateMode     string
}

// NewUpdateService creates a new UpdateService
func NewUpdateService(cache UpdateCache, githubClient GitHubReleaseClient, version, buildType string) *UpdateService {
	return &UpdateService{
		cache:          cache,
		githubClient:   githubClient,
		currentVersion: version,
		buildType:      buildType,
		currentCommit:  os.Getenv("SUB2API_BUILD_COMMIT"),
		updateMode:     os.Getenv("UPDATE_MODE"),
	}
}

func NewUpdateServiceWithCommit(cache UpdateCache, githubClient GitHubReleaseClient, version, buildType, commit string) *UpdateService {
	svc := NewUpdateService(cache, githubClient, version, buildType)
	svc.currentCommit = strings.TrimSpace(commit)
	return svc
}

// UpdateInfo contains update information
type UpdateInfo struct {
	CurrentVersion string       `json:"current_version"`
	LatestVersion  string       `json:"latest_version"`
	HasUpdate      bool         `json:"has_update"`
	ReleaseInfo    *ReleaseInfo `json:"release_info,omitempty"`
	Cached         bool         `json:"cached"`
	Warning        string       `json:"warning,omitempty"`
	BuildType      string       `json:"build_type"` // "source" or "release"
	CurrentCommit  string       `json:"current_commit,omitempty"`
	LatestCommit   string       `json:"latest_commit,omitempty"`
	UpdateMode     string       `json:"update_mode,omitempty"`
	Branch         string       `json:"branch,omitempty"`
	Staged         bool         `json:"staged"`
}

type BranchCommit struct {
	SHA     string `json:"sha"`
	HTMLURL string `json:"html_url"`
	Commit  struct {
		Message string `json:"message"`
		Author  struct {
			Date string `json:"date"`
		} `json:"author"`
	} `json:"commit"`
}

var ErrDockerRollbackNotSupported = infraerrors.BadRequest("DOCKER_ROLLBACK_NOT_SUPPORTED", "Docker updates can only use a published custom image")

// ReleaseInfo contains GitHub release details
type ReleaseInfo struct {
	Name        string  `json:"name"`
	Body        string  `json:"body"`
	PublishedAt string  `json:"published_at"`
	HTMLURL     string  `json:"html_url"`
	Assets      []Asset `json:"assets,omitempty"`
}

// Asset represents a release asset
type Asset struct {
	Name        string `json:"name"`
	DownloadURL string `json:"download_url"`
	Size        int64  `json:"size"`
}

// GitHubRelease represents GitHub API response
type GitHubRelease struct {
	TagName     string        `json:"tag_name"`
	Name        string        `json:"name"`
	Body        string        `json:"body"`
	PublishedAt string        `json:"published_at"`
	HTMLURL     string        `json:"html_url"`
	Draft       bool          `json:"draft"`
	Prerelease  bool          `json:"prerelease"`
	Assets      []GitHubAsset `json:"assets"`
}

// RollbackVersion describes a release version the system can roll back to
type RollbackVersion struct {
	Version     string `json:"version"` // without "v" prefix, e.g. "0.1.146"
	PublishedAt string `json:"published_at"`
	HTMLURL     string `json:"html_url"`
}

type GitHubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// CheckUpdate checks for available updates
func (s *UpdateService) CheckUpdate(ctx context.Context, force bool) (*UpdateInfo, error) {
	if s.updateMode == "docker" {
		return s.checkDockerUpdate(ctx, force)
	}
	// Try cache first
	if !force {
		if cached, err := s.getFromCache(ctx); err == nil && cached != nil {
			return cached, nil
		}
	}

	// Fetch from GitHub
	info, err := s.fetchLatestRelease(ctx)
	if err != nil {
		// Return cached on error
		if cached, cacheErr := s.getFromCache(ctx); cacheErr == nil && cached != nil {
			cached.Warning = "Using cached data: " + err.Error()
			return cached, nil
		}
		return &UpdateInfo{
			CurrentVersion: s.currentVersion,
			LatestVersion:  s.currentVersion,
			HasUpdate:      false,
			Warning:        err.Error(),
			BuildType:      s.buildType,
		}, nil
	}

	// Cache result
	s.saveToCache(ctx, info)
	return info, nil
}

func (s *UpdateService) checkDockerUpdate(ctx context.Context, force bool) (*UpdateInfo, error) {
	const defaultRepo = "yunyar/sub2api"
	repo := os.Getenv("UPDATE_GITHUB_REPO")
	if repo == "" {
		repo = defaultRepo
	}
	branch := os.Getenv("UPDATE_GITHUB_BRANCH")
	if branch == "" {
		branch = "custom/community-qrcode"
	}
	info := &UpdateInfo{CurrentVersion: s.currentVersion, CurrentCommit: s.currentCommit, BuildType: s.buildType, UpdateMode: "docker", Branch: branch}
	if _, staged, stageErr := s.dockerStagedImage(); stageErr != nil {
		addUpdateWarning(info, "staged Docker state unavailable: "+stageErr.Error())
	} else if staged {
		info.Staged = true
	}
	if cached, err := s.getFromCache(ctx); !force && err == nil && cached != nil {
		return cached, nil
	}
	client, ok := s.githubClient.(CustomBranchClient)
	if !ok {
		addUpdateWarning(info, "branch update client is unavailable")
		return info, nil
	}
	latest, err := client.FetchBranchCommit(ctx, repo, branch)
	if err != nil {
		addUpdateWarning(info, err.Error())
		return info, nil
	}
	if !isFullCommitSHA(latest.SHA) {
		addUpdateWarning(info, "branch returned an invalid commit SHA")
		return info, nil
	}
	publisher, ok := s.githubClient.(PublishedCustomImageClient)
	if !ok {
		addUpdateWarning(info, "custom image publication check is unavailable")
		return info, nil
	}
	published, err := publisher.HasPublishedCustomImage(ctx, repo, branch, latest.SHA)
	if err != nil {
		addUpdateWarning(info, "custom image publication check failed: "+err.Error())
		return info, nil
	}
	if !published {
		addUpdateWarning(info, "custom image is not published for the latest branch commit")
		return info, nil
	}
	info.LatestCommit = latest.SHA
	info.LatestVersion = latest.SHA[:minInt(12, len(latest.SHA))]
	info.HasUpdate = !strings.EqualFold(latest.SHA, s.currentCommit)
	info.ReleaseInfo = &ReleaseInfo{Name: latest.Commit.Message, Body: latest.Commit.Message, PublishedAt: latest.Commit.Author.Date, HTMLURL: latest.HTMLURL}
	s.saveToCache(ctx, info)
	return info, nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func isFullCommitSHA(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') && (char < 'A' || char > 'F') {
			return false
		}
	}
	return true
}

func dockerStageFile() string {
	path := strings.TrimSpace(os.Getenv("UPDATE_DOCKER_STAGE_FILE"))
	if path == "" {
		path = "/app/data/update-staged"
	}
	return path
}

func addUpdateWarning(info *UpdateInfo, warning string) {
	if info.Warning == "" {
		info.Warning = warning
		return
	}
	info.Warning += "; " + warning
}

func (s *UpdateService) dockerStagedImage() (string, bool, error) {
	if stateDir := strings.TrimSpace(os.Getenv("UPDATE_DOCKER_HOST_STATE_DIR")); stateDir != "" {
		stateFile := filepath.Join(stateDir, filepath.Base(dockerComposeFile())+".update-stage")
		data, err := os.ReadFile(stateFile)
		if os.IsNotExist(err) {
			_ = os.Remove(dockerStageFile())
			return "", false, nil
		}
		if err != nil {
			return "", false, err
		}
		image := strings.TrimSpace(string(data))
		if !isCustomImageReference(image) {
			return "", false, fmt.Errorf("host staged image is invalid")
		}
		return image, true, nil
	}
	data, err := os.ReadFile(dockerStageFile())
	if err != nil {
		return "", false, nil
	}
	image := strings.TrimSpace(string(data))
	if !isCustomImageReference(image) {
		return "", false, nil
	}
	stagedCommit := strings.TrimPrefix(image[strings.LastIndex(image, ":")+1:], "custom-")
	if strings.EqualFold(stagedCommit, s.currentCommit) {
		_ = os.Remove(dockerStageFile())
		return "", false, nil
	}
	return image, true, nil
}

func (s *UpdateService) performDockerUpdate(ctx context.Context) error {
	info, err := s.checkDockerUpdate(ctx, true)
	if err != nil {
		return err
	}
	if !info.HasUpdate {
		return ErrNoUpdateAvailable
	}
	image := os.Getenv("UPDATE_DOCKER_IMAGE")
	if image == "" {
		image = "ghcr.io/yunyar/sub2api"
	}
	if !isFullCommitSHA(info.LatestCommit) {
		return fmt.Errorf("invalid latest commit")
	}
	if err := validateDockerUpdatePaths(); err != nil {
		return err
	}
	tag := image + ":custom-" + info.LatestCommit
	if err := dockerCommandRunner(ctx, "pull", tag); err != nil {
		return fmt.Errorf("pull image: %w", err)
	}
	hostDir := dockerHostDeployDir()
	compose := dockerComposeFile()
	serviceName := dockerComposeService()
	args := []string{"run", "--rm",
		"-v", "/var/run/docker.sock:/var/run/docker.sock",
		"-v", hostDir + ":" + hostDir,
	}
	args = append(args, dockerUpdateHelperEnv()...)
	args = append(args,
		"--entrypoint", "/app/docker-update-helper.sh", tag,
		"stage", tag, compose, serviceName)
	if err := dockerCommandRunner(ctx, args...); err != nil {
		return fmt.Errorf("stage compose update: %w", err)
	}
	if err := os.WriteFile(dockerStageFile(), []byte(tag+"\n"), 0600); err != nil {
		return fmt.Errorf("write staged update state: %w", err)
	}
	return nil
}

var dockerCommandRunner = runDockerCommand

func runDockerCommand(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Env = append(os.Environ(), "DOCKER_CLI_HINTS=false")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// PerformUpdate downloads and applies the update
// Uses atomic file replacement pattern for safe in-place updates
func (s *UpdateService) PerformUpdate(ctx context.Context) error {
	if s.updateMode == "docker" {
		return s.performDockerUpdate(ctx)
	}
	info, err := s.CheckUpdate(ctx, true)
	if err != nil {
		return err
	}

	if !info.HasUpdate {
		return ErrNoUpdateAvailable
	}

	return s.applyReleaseAssets(ctx, info.ReleaseInfo.Assets)
}

func (s *UpdateService) RestartDocker(ctx context.Context) error {
	if s.updateMode != "docker" {
		return fmt.Errorf("docker update mode is disabled")
	}
	compose := dockerComposeFile()
	serviceName := dockerComposeService()
	helperImage, staged, err := s.dockerStagedImage()
	if err != nil {
		return fmt.Errorf("read staged Docker state: %w", err)
	}
	if !staged {
		return fmt.Errorf("no downloaded update is waiting to be applied")
	}
	if err := validateDockerUpdatePaths(); err != nil {
		return err
	}
	name := "sub2api-updater-" + strconv.FormatInt(time.Now().Unix(), 10)
	args := []string{"run", "--rm", "-d", "--name", name,
		"-v", "/var/run/docker.sock:/var/run/docker.sock",
		"-v", dockerHostDeployDir() + ":" + dockerHostDeployDir(),
	}
	args = append(args, dockerUpdateHelperEnv()...)
	args = append(args,
		"--entrypoint", "/app/docker-update-helper.sh", helperImage,
		"activate", helperImage, compose, serviceName)
	if err := dockerCommandRunner(ctx, args...); err != nil {
		return fmt.Errorf("start update helper: %w", err)
	}
	return nil
}

func (s *UpdateService) DockerUpdateEnabled() bool { return s.updateMode == "docker" }

func isCustomImageReference(image string) bool {
	tag := image[strings.LastIndex(image, ":")+1:]
	return strings.HasPrefix(tag, "custom-") && isFullCommitSHA(strings.TrimPrefix(tag, "custom-"))
}

func dockerHostDeployDir() string {
	if value := strings.TrimSpace(os.Getenv("UPDATE_DOCKER_HOST_DEPLOY_DIR")); value != "" {
		return value
	}
	return "/root/sub2api-deploy"
}

func dockerComposeFile() string {
	if value := strings.TrimSpace(os.Getenv("UPDATE_DOCKER_COMPOSE_FILE")); value != "" {
		return value
	}
	return filepath.Join(dockerHostDeployDir(), "docker-compose.yml")
}

func dockerComposeService() string {
	if value := strings.TrimSpace(os.Getenv("UPDATE_DOCKER_COMPOSE_SERVICE")); value != "" {
		return value
	}
	return "sub2api"
}

func dockerUpdateHelperEnv() []string {
	return []string{
		"-e", "UPDATE_DOCKER_COMPOSE_OVERLAY_FILE=" + strings.TrimSpace(os.Getenv("UPDATE_DOCKER_COMPOSE_OVERLAY_FILE")),
		"-e", "UPDATE_DOCKER_COMPOSE_PROJECT_NAME=" + strings.TrimSpace(os.Getenv("UPDATE_DOCKER_COMPOSE_PROJECT_NAME")),
		"-e", "UPDATE_DOCKER_HEALTH_TIMEOUT_SECONDS=" + strings.TrimSpace(os.Getenv("UPDATE_DOCKER_HEALTH_TIMEOUT_SECONDS")),
	}
}

func validateDockerUpdatePaths() error {
	hostDir := dockerHostDeployDir()
	if !filepath.IsAbs(hostDir) {
		return fmt.Errorf("Docker deployment directory must be absolute")
	}
	for _, path := range []string{dockerComposeFile(), strings.TrimSpace(os.Getenv("UPDATE_DOCKER_COMPOSE_OVERLAY_FILE"))} {
		if path == "" {
			continue
		}
		if !filepath.IsAbs(path) {
			return fmt.Errorf("Docker compose file paths must be absolute")
		}
		relativePath, err := filepath.Rel(filepath.Clean(hostDir), filepath.Clean(path))
		if err != nil || relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
			return fmt.Errorf("Docker compose file must be inside the deployment directory")
		}
	}
	return nil
}

// applyReleaseAssets downloads the platform archive from the given release assets,
// verifies its checksum, and atomically swaps the running binary.
// Shared by PerformUpdate (latest) and RollbackToVersion (specific older version).
func (s *UpdateService) applyReleaseAssets(ctx context.Context, releaseAssets []Asset) error {
	// Find matching archive and checksum for current platform
	archiveName := s.getArchiveName()
	var downloadURL string
	var checksumURL string

	for _, asset := range releaseAssets {
		if strings.Contains(asset.Name, archiveName) && !strings.HasSuffix(asset.Name, ".txt") {
			downloadURL = asset.DownloadURL
		}
		if asset.Name == "checksums.txt" {
			checksumURL = asset.DownloadURL
		}
	}

	if downloadURL == "" {
		return fmt.Errorf("no compatible release found for %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	// SECURITY: Validate download URL is from trusted domain
	if err := validateDownloadURL(downloadURL); err != nil {
		return fmt.Errorf("invalid download URL: %w", err)
	}
	if checksumURL != "" {
		if err := validateDownloadURL(checksumURL); err != nil {
			return fmt.Errorf("invalid checksum URL: %w", err)
		}
	}

	// Get current executable path
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return fmt.Errorf("failed to resolve symlinks: %w", err)
	}

	exeDir := filepath.Dir(exePath)

	// Create temp directory in the SAME directory as executable
	// This ensures os.Rename is atomic (same filesystem)
	tempDir, err := os.MkdirTemp(exeDir, ".sub2api-update-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Download archive
	archivePath := filepath.Join(tempDir, filepath.Base(downloadURL))
	if err := s.downloadFile(ctx, downloadURL, archivePath); err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	// Verify checksum if available
	if checksumURL != "" {
		if err := s.verifyChecksum(ctx, archivePath, checksumURL); err != nil {
			return fmt.Errorf("checksum verification failed: %w", err)
		}
	}

	// Extract binary from archive
	newBinaryPath := filepath.Join(tempDir, "sub2api")
	if err := s.extractBinary(archivePath, newBinaryPath); err != nil {
		return fmt.Errorf("extraction failed: %w", err)
	}

	// Set executable permission before replacement
	if err := os.Chmod(newBinaryPath, 0755); err != nil {
		return fmt.Errorf("chmod failed: %w", err)
	}

	// Atomic replacement using rename pattern:
	// 1. Rename current -> backup (atomic on Unix)
	// 2. Rename new -> current (atomic on Unix, same filesystem)
	// If step 2 fails, restore backup
	backupPath := exePath + ".backup"

	// Remove old backup if exists
	_ = os.Remove(backupPath)

	// Step 1: Move current binary to backup
	if err := os.Rename(exePath, backupPath); err != nil {
		return fmt.Errorf("backup failed: %w", err)
	}

	// Step 2: Move new binary to target location (atomic, same filesystem)
	if err := os.Rename(newBinaryPath, exePath); err != nil {
		// Restore backup on failure
		if restoreErr := os.Rename(backupPath, exePath); restoreErr != nil {
			return fmt.Errorf("replace failed and restore failed: %w (restore error: %v)", err, restoreErr)
		}
		return fmt.Errorf("replace failed (restored backup): %w", err)
	}

	// Success - backup file is kept for rollback capability
	// It will be cleaned up on next successful update
	return nil
}

// Rollback restores the previous version
func (s *UpdateService) Rollback() error {
	if s.updateMode == "docker" {
		return ErrDockerRollbackNotSupported
	}
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return fmt.Errorf("failed to resolve symlinks: %w", err)
	}

	backupFile := exePath + ".backup"
	if _, err := os.Stat(backupFile); os.IsNotExist(err) {
		return fmt.Errorf("no backup found")
	}

	// Replace current with backup
	if err := os.Rename(backupFile, exePath); err != nil {
		return fmt.Errorf("rollback failed: %w", err)
	}

	return nil
}

// ListRollbackVersions returns up to maxRollbackVersions release versions that are
// strictly older than the current version (the current version itself is excluded),
// newest first. Draft and prerelease entries are skipped.
func (s *UpdateService) ListRollbackVersions(ctx context.Context) ([]RollbackVersion, error) {
	if s.updateMode == "docker" {
		return nil, ErrDockerRollbackNotSupported
	}
	releases, err := s.fetchRollbackCandidates(ctx)
	if err != nil {
		return nil, err
	}

	versions := make([]RollbackVersion, 0, len(releases))
	for _, r := range releases {
		versions = append(versions, RollbackVersion{
			Version:     strings.TrimPrefix(r.TagName, "v"),
			PublishedAt: r.PublishedAt,
			HTMLURL:     r.HTMLURL,
		})
	}
	return versions, nil
}

// RollbackToVersion downloads and installs a specific older version.
// The target must be one of the versions returned by ListRollbackVersions;
// anything else (including the current version) is rejected.
func (s *UpdateService) RollbackToVersion(ctx context.Context, version string) error {
	if s.updateMode == "docker" {
		return ErrDockerRollbackNotSupported
	}
	target := strings.TrimPrefix(strings.TrimSpace(version), "v")
	if target == "" {
		return ErrRollbackVersionNotAllowed
	}

	releases, err := s.fetchRollbackCandidates(ctx)
	if err != nil {
		return err
	}

	var match *GitHubRelease
	for _, r := range releases {
		if strings.TrimPrefix(r.TagName, "v") == target {
			match = r
			break
		}
	}
	if match == nil {
		return ErrRollbackVersionNotAllowed
	}

	assets := make([]Asset, len(match.Assets))
	for i, a := range match.Assets {
		assets[i] = Asset{
			Name:        a.Name,
			DownloadURL: a.BrowserDownloadURL,
			Size:        a.Size,
		}
	}

	return s.applyReleaseAssets(ctx, assets)
}

// fetchRollbackCandidates fetches recent releases and keeps the newest
// maxRollbackVersions entries strictly older than the current version.
func (s *UpdateService) fetchRollbackCandidates(ctx context.Context) ([]*GitHubRelease, error) {
	releases, err := s.githubClient.FetchRecentReleases(ctx, githubRepo, rollbackFetchPageSize)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool, len(releases))
	candidates := make([]*GitHubRelease, 0, maxRollbackVersions)
	for _, r := range releases {
		if r == nil || r.Draft || r.Prerelease {
			continue
		}
		v := strings.TrimPrefix(r.TagName, "v")
		if v == "" || seen[v] {
			continue
		}
		// Only versions strictly older than current (also excludes current itself)
		if compareVersions(v, s.currentVersion) >= 0 {
			continue
		}
		seen[v] = true
		candidates = append(candidates, r)
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		return compareVersions(
			strings.TrimPrefix(candidates[i].TagName, "v"),
			strings.TrimPrefix(candidates[j].TagName, "v"),
		) > 0
	})

	if len(candidates) > maxRollbackVersions {
		candidates = candidates[:maxRollbackVersions]
	}
	return candidates, nil
}

func (s *UpdateService) fetchLatestRelease(ctx context.Context) (*UpdateInfo, error) {
	release, err := s.githubClient.FetchLatestRelease(ctx, githubRepo)
	if err != nil {
		return nil, err
	}

	latestVersion := strings.TrimPrefix(release.TagName, "v")

	assets := make([]Asset, len(release.Assets))
	for i, a := range release.Assets {
		assets[i] = Asset{
			Name:        a.Name,
			DownloadURL: a.BrowserDownloadURL,
			Size:        a.Size,
		}
	}

	return &UpdateInfo{
		CurrentVersion: s.currentVersion,
		LatestVersion:  latestVersion,
		HasUpdate:      compareVersions(s.currentVersion, latestVersion) < 0,
		ReleaseInfo: &ReleaseInfo{
			Name:        release.Name,
			Body:        release.Body,
			PublishedAt: release.PublishedAt,
			HTMLURL:     release.HTMLURL,
			Assets:      assets,
		},
		Cached:    false,
		BuildType: s.buildType,
	}, nil
}

func (s *UpdateService) downloadFile(ctx context.Context, downloadURL, dest string) error {
	return s.githubClient.DownloadFile(ctx, downloadURL, dest, maxDownloadSize)
}

func (s *UpdateService) getArchiveName() string {
	osName := runtime.GOOS
	arch := runtime.GOARCH
	return fmt.Sprintf("%s_%s", osName, arch)
}

// validateDownloadURL checks if the URL is from an allowed domain
// SECURITY: This prevents SSRF and ensures downloads only come from trusted GitHub domains
func validateDownloadURL(rawURL string) error {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Must be HTTPS
	if parsedURL.Scheme != "https" {
		return fmt.Errorf("only HTTPS URLs are allowed")
	}

	// Check against allowed hosts
	host := parsedURL.Host
	// GitHub release URLs can be from github.com or objects.githubusercontent.com
	if host != allowedDownloadHost &&
		!strings.HasSuffix(host, "."+allowedDownloadHost) &&
		host != allowedAssetHost &&
		!strings.HasSuffix(host, "."+allowedAssetHost) {
		return fmt.Errorf("download from untrusted host: %s", host)
	}

	return nil
}

func (s *UpdateService) verifyChecksum(ctx context.Context, filePath, checksumURL string) error {
	// Download checksums file
	checksumData, err := s.githubClient.FetchChecksumFile(ctx, checksumURL)
	if err != nil {
		return fmt.Errorf("failed to download checksums: %w", err)
	}

	// Calculate file hash
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	actualHash := hex.EncodeToString(h.Sum(nil))

	// Find expected hash in checksums file
	fileName := filepath.Base(filePath)
	scanner := bufio.NewScanner(strings.NewReader(string(checksumData)))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) == 2 && parts[1] == fileName {
			if parts[0] == actualHash {
				return nil
			}
			return fmt.Errorf("checksum mismatch: expected %s, got %s", parts[0], actualHash)
		}
	}

	return fmt.Errorf("checksum not found for %s", fileName)
}

func (s *UpdateService) extractBinary(archivePath, destPath string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	var reader io.Reader = f

	// Handle gzip compression
	if strings.HasSuffix(archivePath, ".gz") || strings.HasSuffix(archivePath, ".tar.gz") || strings.HasSuffix(archivePath, ".tgz") {
		gzr, err := gzip.NewReader(f)
		if err != nil {
			return err
		}
		defer func() { _ = gzr.Close() }()
		reader = gzr
	}

	// Handle tar archive
	if strings.Contains(archivePath, ".tar") {
		tr := tar.NewReader(reader)
		for {
			hdr, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}

			// SECURITY: Prevent Zip Slip / Path Traversal attack
			// Only allow files with safe base names, no directory traversal
			baseName := filepath.Base(hdr.Name)

			// Check for path traversal attempts
			if strings.Contains(hdr.Name, "..") {
				return fmt.Errorf("path traversal attempt detected: %s", hdr.Name)
			}

			// Validate the entry is a regular file
			if hdr.Typeflag != tar.TypeReg {
				continue // Skip directories and special files
			}

			// Only extract the specific binary we need
			if baseName == "sub2api" || baseName == "sub2api.exe" {
				// Additional security: limit file size (max 500MB)
				const maxBinarySize = 500 * 1024 * 1024
				if hdr.Size > maxBinarySize {
					return fmt.Errorf("binary too large: %d bytes (max %d)", hdr.Size, maxBinarySize)
				}

				out, err := os.Create(destPath)
				if err != nil {
					return err
				}

				// Use LimitReader to prevent decompression bombs
				limited := io.LimitReader(tr, maxBinarySize)
				if _, err := io.Copy(out, limited); err != nil {
					_ = out.Close()
					return err
				}
				if err := out.Close(); err != nil {
					return err
				}
				return nil
			}
		}
		return fmt.Errorf("binary not found in archive")
	}

	// Direct copy for non-tar files (with size limit)
	const maxBinarySize = 500 * 1024 * 1024
	out, err := os.Create(destPath)
	if err != nil {
		return err
	}

	limited := io.LimitReader(reader, maxBinarySize)
	if _, err := io.Copy(out, limited); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func (s *UpdateService) getFromCache(ctx context.Context) (*UpdateInfo, error) {
	data, err := s.cache.GetUpdateInfo(ctx)
	if err != nil {
		return nil, err
	}

	var cached struct {
		Latest       string       `json:"latest"`
		ReleaseInfo  *ReleaseInfo `json:"release_info"`
		LatestCommit string       `json:"latest_commit"`
		UpdateMode   string       `json:"update_mode"`
		Branch       string       `json:"branch"`
		CacheMode    string       `json:"cache_mode"`
		Staged       bool         `json:"staged"`
		Timestamp    int64        `json:"timestamp"`
	}
	if err := json.Unmarshal([]byte(data), &cached); err != nil {
		return nil, err
	}

	if time.Now().Unix()-cached.Timestamp > updateCacheTTL {
		return nil, fmt.Errorf("cache expired")
	}
	if cached.CacheMode != s.cacheMode() {
		return nil, fmt.Errorf("cache mode mismatch")
	}
	hasUpdate := compareVersions(s.currentVersion, cached.Latest) < 0
	if cached.UpdateMode == "docker" {
		hasUpdate = cached.LatestCommit != "" && !strings.EqualFold(cached.LatestCommit, s.currentCommit)
		_, cached.Staged, err = s.dockerStagedImage()
		if err != nil {
			cached.Staged = false
		}
	}

	return &UpdateInfo{
		CurrentVersion: s.currentVersion,
		LatestVersion:  cached.Latest,
		HasUpdate:      hasUpdate,
		ReleaseInfo:    cached.ReleaseInfo,
		Cached:         true,
		BuildType:      s.buildType,
		CurrentCommit:  s.currentCommit,
		LatestCommit:   cached.LatestCommit,
		UpdateMode:     cached.UpdateMode,
		Branch:         cached.Branch,
		Staged:         cached.Staged,
		Warning:        dockerStageWarning(err),
	}, nil
}

func dockerStageWarning(err error) string {
	if err == nil {
		return ""
	}
	return "staged Docker state unavailable: " + err.Error()
}

func (s *UpdateService) cacheMode() string {
	if s.updateMode == "docker" {
		return "docker:" + strings.TrimSpace(os.Getenv("UPDATE_GITHUB_REPO")) + ":" + strings.TrimSpace(os.Getenv("UPDATE_GITHUB_BRANCH"))
	}
	return "release"
}

func (s *UpdateService) saveToCache(ctx context.Context, info *UpdateInfo) {
	cacheData := struct {
		Latest       string       `json:"latest"`
		ReleaseInfo  *ReleaseInfo `json:"release_info"`
		LatestCommit string       `json:"latest_commit"`
		UpdateMode   string       `json:"update_mode"`
		Branch       string       `json:"branch"`
		CacheMode    string       `json:"cache_mode"`
		Staged       bool         `json:"staged"`
		Timestamp    int64        `json:"timestamp"`
	}{
		Latest:       info.LatestVersion,
		ReleaseInfo:  info.ReleaseInfo,
		LatestCommit: info.LatestCommit,
		UpdateMode:   info.UpdateMode,
		Branch:       info.Branch,
		CacheMode:    s.cacheMode(),
		Staged:       info.Staged,
		Timestamp:    time.Now().Unix(),
	}

	data, _ := json.Marshal(cacheData)
	_ = s.cache.SetUpdateInfo(ctx, string(data), time.Duration(updateCacheTTL)*time.Second)
}

// compareVersions compares two semantic versions
func compareVersions(current, latest string) int {
	currentParts := parseVersion(current)
	latestParts := parseVersion(latest)

	for i := 0; i < 3; i++ {
		if currentParts[i] < latestParts[i] {
			return -1
		}
		if currentParts[i] > latestParts[i] {
			return 1
		}
	}
	return 0
}

func parseVersion(v string) [3]int {
	v = strings.TrimPrefix(v, "v")
	if idx := strings.IndexByte(v, '-'); idx != -1 {
		v = v[:idx]
	}
	parts := strings.Split(v, ".")
	result := [3]int{0, 0, 0}
	for i := 0; i < len(parts) && i < 3; i++ {
		if parsed, err := strconv.Atoi(parts[i]); err == nil {
			result[i] = parsed
		}
	}
	return result
}
