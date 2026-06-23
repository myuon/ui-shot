package config

import "os"

// Resolved holds the effective settings after applying the precedence:
//
//	command-line flags > environment variables > global config
type Resolved struct {
	Provider  string
	Bucket    string
	BaseURL   string
	ProjectID string // GCS project id
	Profile   string // S3 profile
	AccountID string // R2 account id
}

// Source values for a single setting, in precedence order. The first
// non-empty value wins.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// Resolve computes the effective settings from flags, environment, and the
// loaded config, following the documented precedence order.
//
// flagX arguments are the raw command-line flag values (empty if unset).
func Resolve(cfg *Config, flagProvider, flagBucket, flagBaseURL, flagProject, flagProfile, flagAccountID string) Resolved {
	provider := firstNonEmpty(
		flagProvider,
		os.Getenv("UISHOT_PROVIDER"),
		cfg.Provider,
	)

	r := Resolved{Provider: provider}

	// Bucket / base URL share common env vars across providers, but the
	// config default depends on the resolved provider.
	var cfgBucket, cfgBaseURL, cfgProject, cfgProfile, cfgAccountID string
	switch provider {
	case "gcs":
		cfgBucket = cfg.GCS.Bucket
		cfgBaseURL = cfg.GCS.BaseURL
		cfgProject = cfg.GCS.ProjectID
	case "s3":
		cfgBucket = cfg.S3.Bucket
		cfgBaseURL = cfg.S3.BaseURL
		cfgProfile = cfg.S3.Profile
	case "r2":
		cfgBucket = cfg.R2.Bucket
		cfgBaseURL = cfg.R2.BaseURL
		cfgAccountID = cfg.R2.AccountID
	}

	r.Bucket = firstNonEmpty(flagBucket, os.Getenv("UISHOT_BUCKET"), cfgBucket)
	r.BaseURL = firstNonEmpty(flagBaseURL, os.Getenv("UISHOT_BASE_URL"), cfgBaseURL)
	r.ProjectID = firstNonEmpty(flagProject, os.Getenv("UISHOT_GCS_PROJECT_ID"), cfgProject)
	r.Profile = firstNonEmpty(flagProfile, os.Getenv("AWS_PROFILE"), cfgProfile)
	r.AccountID = firstNonEmpty(flagAccountID, os.Getenv("UISHOT_R2_ACCOUNT_ID"), cfgAccountID)

	return r
}
