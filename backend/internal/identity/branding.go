package identity

import (
	"context"
	"fmt"

	"github.com/zitadel/zitadel-go/v3/pkg/client/middleware"
	"github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/management"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Brand returns the Malgary look applied to every tenant's sign-in page (Principle X). Logo
// upload goes through Zitadel's asset API and is done once per org from deploy/branding/.
func Brand() *management.AddCustomLabelPolicyRequest {
	return &management.AddCustomLabelPolicyRequest{
		PrimaryColor:        "#0a7766",
		BackgroundColor:     "#f5f6f4",
		WarnColor:           "#b3261e",
		FontColor:           "#1c2320",
		PrimaryColorDark:    "#3fc8af",
		BackgroundColorDark: "#121614",
		WarnColorDark:       "#f2b8b5",
		FontColorDark:       "#e3e8e5",
		HideLoginNameSuffix: true,
		DisableWatermark:    true,
	}
}

// ApplyBranding sets and activates Malgary's label policy on the tenant's org.
func (z *Zitadel) ApplyBranding(ctx context.Context, orgID string) error {
	ctx = middleware.SetOrgID(ctx, orgID)
	mgmt := z.c.ManagementService()
	if _, err := mgmt.AddCustomLabelPolicy(ctx, Brand()); err != nil && status.Code(err) != codes.AlreadyExists {
		return fmt.Errorf("label policy: %w", err)
	}
	if _, err := mgmt.ActivateCustomLabelPolicy(ctx, &management.ActivateCustomLabelPolicyRequest{}); err != nil {
		return fmt.Errorf("activate label policy: %w", err)
	}
	return nil
}
