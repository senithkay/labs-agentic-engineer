package seed

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"gorm.io/gorm"

	"github.com/wso2/asdlc/git-service/config"
	"github.com/wso2/asdlc/git-service/models"
	"github.com/wso2/asdlc/git-service/pkg/credentials"
	"github.com/wso2/asdlc/git-service/services"
)

// DefaultOrgPATFromEnv seeds a PAT-mode credential row for one or more
// OC orgs from env vars. Dev-tier only, env-gated, idempotent.
//
// The list of orgs to seed comes from `GITHUB_PLATFORM_PAT_SEED_ORGS`
// (comma-separated, default "default"). Multi-org seeding lets dev
// clusters bootstrap orgs other than "default" — e.g. when the IDP
// returns an admin user with subject claim `admin`, the BFF passes
// `admin` as the OC org id and we need a credential row for it too.
//
// Routes through CredentialService.Connect (the same path the console
// UI uses) — there is no parallel PAT-validation logic. The seed is
// just a tier/env/idempotency-gated invocation of the existing connect
// flow with credentials lifted from env vars.
//
// Production deployments leave GITHUB_PLATFORM_PAT/GITHUB_REPO_OWNER
// empty; the seed silently skips, and per-org connect via the console
// remains the only credential entry point.
//
// Idempotency policy:
//   - DB row missing                  → full Connect (writes DB + OpenBao).
//   - DB row present + OpenBao value   → skip (steady-state).
//   - DB row present + OpenBao missing → re-write OpenBao only (preserve
//     the operator's status / webhook secrets / disconnect intent on
//     the row, but heal the secret-store state asymmetry that occurs
//     when OpenBao runs in dev mode and is wiped across cluster
//     restarts).
//
// "Status=disconnected" rows are still skipped on the OpenBao re-heal —
// a disconnected org should stay disconnected after a restart.
func DefaultOrgPATFromEnv(
	ctx context.Context,
	db *gorm.DB,
	cfg config.Config,
	credService *services.CredentialService,
	store credentials.OpenBaoStore,
) error {
	if cfg.DeploymentTier != "dev" {
		slog.InfoContext(ctx, "default-org seed: skipped (DeploymentTier != dev)",
			"tier", cfg.DeploymentTier)
		return nil
	}
	if cfg.GitHubPlatformPAT == "" || cfg.GitHubRepoOwner == "" {
		slog.InfoContext(ctx, "default-org seed: skipped (PAT or owner not set)",
			"patSet", cfg.GitHubPlatformPAT != "",
			"ownerSet", cfg.GitHubRepoOwner != "")
		return nil
	}

	orgs := parseSeedOrgs(cfg.GitHubPlatformPATSeedOrgs)
	if len(orgs) == 0 {
		slog.InfoContext(ctx, "default-org seed: skipped (no orgs configured)")
		return nil
	}

	for _, org := range orgs {
		seedOne(ctx, db, cfg, credService, store, org)
	}
	return nil
}

func seedOne(
	ctx context.Context,
	db *gorm.DB,
	cfg config.Config,
	credService *services.CredentialService,
	store credentials.OpenBaoStore,
	orgHandle string,
) {
	var existing models.OrgCredential
	err := db.WithContext(ctx).
		Where("oc_org_id = ?", orgHandle).
		First(&existing).Error

	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		// Fresh — full Connect.
	case err != nil:
		slog.WarnContext(ctx, "org seed: db error",
			"ocOrgId", orgHandle, "error", err)
		return
	default:
		// Row exists. Heal OpenBao if its value is missing (typical
		// after a cluster restart in dev where OpenBao is in-memory).
		// Skip otherwise.
		if existing.Kind != "user-pat" {
			slog.InfoContext(ctx, "org seed: skipped (row exists with non-PAT kind)",
				"ocOrgId", orgHandle, "kind", existing.Kind, "status", existing.Status)
			return
		}
		if existing.Status == "disconnected" {
			slog.InfoContext(ctx, "org seed: skipped (operator-disconnected)",
				"ocOrgId", orgHandle)
			return
		}
		if store == nil {
			slog.InfoContext(ctx, "org seed: skipped (row exists, no store available)",
				"ocOrgId", orgHandle, "kind", existing.Kind, "status", existing.Status)
			return
		}
		val, gErr := store.Get(ctx, orgHandle, "github/pat")
		if gErr == nil && len(val) > 0 {
			slog.InfoContext(ctx, "org seed: skipped (row + openbao both present)",
				"ocOrgId", orgHandle, "kind", existing.Kind, "status", existing.Status)
			return
		}
		if gErr != nil && !errors.Is(gErr, credentials.ErrSecretNotFound) {
			slog.WarnContext(ctx, "org seed: openbao get error",
				"ocOrgId", orgHandle, "error", gErr)
			return
		}
		if pErr := store.Put(ctx, orgHandle, "github/pat", []byte(cfg.GitHubPlatformPAT)); pErr != nil {
			slog.WarnContext(ctx, "org seed: openbao re-heal write failed",
				"ocOrgId", orgHandle, "error", pErr)
			return
		}
		slog.InfoContext(ctx, "org seed: openbao re-healed (db row preserved)",
			"ocOrgId", orgHandle, "kind", existing.Kind)
		return
	}

	proj, err := credService.Connect(ctx, orgHandle, services.ConnectRequest{
		Kind:        "user-pat",
		PAT:         cfg.GitHubPlatformPAT,
		GitHubLogin: cfg.GitHubRepoOwner,
	})
	if err != nil {
		// Don't fail startup — log and continue. A bad PAT shouldn't
		// brick the service for every other org.
		slog.WarnContext(ctx, "org seed: connect failed",
			"ocOrgId", orgHandle, "githubLogin", cfg.GitHubRepoOwner, "error", err)
		return
	}
	slog.InfoContext(ctx, "org seeded",
		"ocOrgId", orgHandle,
		"kind", proj.Kind,
		"githubLogin", proj.GitHubLogin,
		"identityLogin", proj.IdentityLogin)
}

func parseSeedOrgs(raw string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}
