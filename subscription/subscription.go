// Package subscription generates client bundles from a subscription manifest.
package subscription

import "context"

// Generate validates the manifest and writes one bundle per user.
func Generate(ctx context.Context, manifestPath, outputDir, xrayBinary string) (int, error) {
	if _, err := checkManifestWithXray(ctx, manifestPath, xrayBinary); err != nil {
		return 0, err
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return generate(ctx, manifestPath, outputDir)
}

// Check validates a manifest and all generated client variants.
func Check(ctx context.Context, manifestPath, xrayBinary string) (int, error) {
	return checkManifestWithXray(ctx, manifestPath, xrayBinary)
}

// Token generates a random subscription token.
func Token() (string, error) { return newToken() }
