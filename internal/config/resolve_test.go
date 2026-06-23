package config

import "testing"

func TestResolvePrecedence(t *testing.T) {
	cfg := Default()
	cfg.Provider = "gcs"
	cfg.GCS.Bucket = "cfg-bucket"
	cfg.GCS.BaseURL = "https://cfg-base"
	cfg.GCS.ProjectID = "cfg-project"

	t.Run("config only", func(t *testing.T) {
		t.Setenv("UISHOT_PROVIDER", "")
		t.Setenv("UISHOT_BUCKET", "")
		t.Setenv("UISHOT_BASE_URL", "")
		t.Setenv("UISHOT_GCS_PROJECT_ID", "")
		got := Resolve(cfg, "", "", "", "", "", "")
		if got.Provider != "gcs" || got.Bucket != "cfg-bucket" || got.BaseURL != "https://cfg-base" || got.ProjectID != "cfg-project" {
			t.Errorf("config-only resolve wrong: %+v", got)
		}
	})

	t.Run("env overrides config", func(t *testing.T) {
		t.Setenv("UISHOT_PROVIDER", "")
		t.Setenv("UISHOT_BUCKET", "env-bucket")
		t.Setenv("UISHOT_BASE_URL", "https://env-base")
		t.Setenv("UISHOT_GCS_PROJECT_ID", "env-project")
		got := Resolve(cfg, "", "", "", "", "", "")
		if got.Bucket != "env-bucket" || got.BaseURL != "https://env-base" || got.ProjectID != "env-project" {
			t.Errorf("env override wrong: %+v", got)
		}
	})

	t.Run("flag overrides env and config", func(t *testing.T) {
		t.Setenv("UISHOT_BUCKET", "env-bucket")
		t.Setenv("UISHOT_BASE_URL", "https://env-base")
		t.Setenv("UISHOT_GCS_PROJECT_ID", "env-project")
		got := Resolve(cfg, "gcs", "flag-bucket", "https://flag-base", "flag-project", "", "")
		if got.Bucket != "flag-bucket" || got.BaseURL != "https://flag-base" || got.ProjectID != "flag-project" {
			t.Errorf("flag override wrong: %+v", got)
		}
	})
}

func TestResolveProviderSection(t *testing.T) {
	cfg := Default()
	cfg.Provider = "s3"
	cfg.S3.Bucket = "s3-bucket"
	cfg.S3.BaseURL = "https://s3-base"
	cfg.S3.Profile = "prod"

	t.Setenv("UISHOT_PROVIDER", "")
	t.Setenv("UISHOT_BUCKET", "")
	t.Setenv("UISHOT_BASE_URL", "")
	t.Setenv("AWS_PROFILE", "")
	got := Resolve(cfg, "", "", "", "", "", "")
	if got.Provider != "s3" || got.Bucket != "s3-bucket" || got.BaseURL != "https://s3-base" || got.Profile != "prod" {
		t.Errorf("s3 section resolve wrong: %+v", got)
	}
}
