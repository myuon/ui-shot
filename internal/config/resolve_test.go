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

func TestResolveBaseURLExplicit(t *testing.T) {
	cfg := Default()
	cfg.Provider = "gcs"
	cfg.GCS.Bucket = "bucketA"
	cfg.GCS.BaseURL = "https://storage.googleapis.com/bucketA"

	t.Run("base url from config is not explicit", func(t *testing.T) {
		t.Setenv("UISHOT_PROVIDER", "")
		t.Setenv("UISHOT_BUCKET", "")
		t.Setenv("UISHOT_BASE_URL", "")
		got := Resolve(cfg, "", "", "", "", "", "")
		if got.BaseURL != "https://storage.googleapis.com/bucketA" {
			t.Errorf("BaseURL = %q, want config value", got.BaseURL)
		}
		if got.BaseURLExplicit {
			t.Error("BaseURLExplicit = true for config-sourced base URL, want false")
		}
		if got.ConfigBucket != "bucketA" || got.ConfigBaseURL != "https://storage.googleapis.com/bucketA" {
			t.Errorf("config snapshot wrong: ConfigBucket=%q ConfigBaseURL=%q", got.ConfigBucket, got.ConfigBaseURL)
		}
	})

	// Regression for issue #7: re-running setup with a new --bucket but no
	// --base-url must leave BaseURL non-explicit so setup re-derives it from
	// the new bucket instead of keeping the stale config base_url.
	t.Run("new bucket flag without base url stays non-explicit", func(t *testing.T) {
		t.Setenv("UISHOT_PROVIDER", "")
		t.Setenv("UISHOT_BUCKET", "")
		t.Setenv("UISHOT_BASE_URL", "")
		got := Resolve(cfg, "", "bucketB", "", "", "", "")
		if got.Bucket != "bucketB" {
			t.Errorf("Bucket = %q, want bucketB", got.Bucket)
		}
		if got.BaseURLExplicit {
			t.Error("BaseURLExplicit = true when only --bucket changed, want false")
		}
		if got.ConfigBucket != "bucketA" {
			t.Errorf("ConfigBucket = %q, want bucketA", got.ConfigBucket)
		}
	})

	t.Run("base url flag is explicit", func(t *testing.T) {
		t.Setenv("UISHOT_PROVIDER", "")
		t.Setenv("UISHOT_BUCKET", "")
		t.Setenv("UISHOT_BASE_URL", "")
		got := Resolve(cfg, "", "bucketB", "https://cdn.example.com", "", "", "")
		if got.BaseURL != "https://cdn.example.com" {
			t.Errorf("BaseURL = %q, want flag value", got.BaseURL)
		}
		if !got.BaseURLExplicit {
			t.Error("BaseURLExplicit = false for --base-url flag, want true")
		}
	})

	t.Run("base url env var is explicit", func(t *testing.T) {
		t.Setenv("UISHOT_PROVIDER", "")
		t.Setenv("UISHOT_BUCKET", "")
		t.Setenv("UISHOT_BASE_URL", "https://env.example.com")
		got := Resolve(cfg, "", "", "", "", "", "")
		if got.BaseURL != "https://env.example.com" {
			t.Errorf("BaseURL = %q, want env value", got.BaseURL)
		}
		if !got.BaseURLExplicit {
			t.Error("BaseURLExplicit = false for UISHOT_BASE_URL env, want true")
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
