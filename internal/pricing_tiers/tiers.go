// Package pricing_tiers is the single source of truth for InfraMind commercial
// tiers, prices, and conversion URLs. All CTAs (CLI, docs, README) must
// re-import from here so the product presents one consistent story.
package pricing_tiers

// Tier describes a single commercial plan surfaced to the user.
type Tier struct {
	ID         string
	Name       string
	Price      string
	Target     string
	Quota      string
	CTA        string
	URL        string
	Highlights []string
}

// Solo is the self-serve plan for individual operators (freelancers, single-sub CTOs).
// The feature list reflects what ships with InfraMind Cloud v1.1 — the CLI
// Free tier already covers discovery, simulation, and history.
var Solo = Tier{
	ID:     "solo",
	Name:   "Solo",
	Price:  "$29/mo (v1.1)",
	Target: "Freelancer or single-sub CTO",
	Quota:  "3 subscriptions · 90-day history · white-label export (v1.1)",
	CTA:    "Join founding waitlist",
	URL:    CheckoutSoloURL,
	Highlights: []string{
		"One-click auto-apply for low-risk recommendations (v1.1)",
		"Export branded reports (PDF/MD) per client (v1.1)",
		"Scheduled weekly scans with email digest (v1.1)",
	},
}

// Team is the flat-rate plan for startups and scale-ups.
var Team = Tier{
	ID:     "team",
	Name:   "Team",
	Price:  "$199/mo (v1.1)",
	Target: "Startup / scale-up (up to 10 seats)",
	Quota:  "10 subscriptions · Slack alerts · scheduled scans · SSO (v1.1)",
	CTA:    "Join founding waitlist",
	URL:    CheckoutTeamURL,
	Highlights: []string{
		"Everything in Solo",
		"Shared policy templates + role-based access (v1.1)",
		"Slack/Teams alerts on drift or new waste (v1.1)",
	},
}

// Enterprise is the anchor-priced plan with a performance-based alternative.
var Enterprise = Tier{
	ID:     "enterprise",
	Name:   "Enterprise",
	Price:  "from $799/mo or 8% of verified savings (greater applies, v1.1)",
	Target: "Mid-market / regulated workloads",
	Quota:  "Unlimited subs · SAML (in design) · audit log (in design) · SOC2 roadmap",
	CTA:    "Book a discovery call",
	URL:    EnterpriseURL,
	Highlights: []string{
		"Unlimited subscriptions and seats (v1.1)",
		"SAML SSO (in design), audit log (in design), SOC2 roadmap",
		"Performance-based pricing available (pay only if we prove savings)",
	},
}

// Partner is the MSP/consultant track with revenue share and white-labeling.
var Partner = Tier{
	ID:     "partner",
	Name:   "Partner / MSP",
	Price:  "20% recurring revshare (v1.1)",
	Target: "Consultants and MSPs managing multiple clients",
	Quota:  "Per-client reports · custom branding · co-marketing (v1.1)",
	CTA:    "Join partner waitlist",
	URL:    PartnerURL,
	Highlights: []string{
		"White-label PDF reports with your brand (v1.1)",
		"Per-client subscription management (v1.1)",
		"20% recurring revenue share on referrals",
	},
}

// All returns the tiers in the canonical display order.
func All() []Tier {
	return []Tier{Solo, Team, Enterprise, Partner}
}

// Conversion URLs.
//
// While Cloud (v1.1) is in early-access, every paid tier points to the
// same waitlist fragment instead of a 404 checkout path. When the real
// landing pages exist we'll flip the constants below to Stripe Checkout
// / Calendly / Typeform URLs and every CTA call-site updates
// automatically — nothing else in the codebase hard-codes these strings.
//
// The "real" target for each tier is preserved as a comment so future
// maintainers don't have to re-derive the routing.
const (
	// CheckoutSoloURL eventually points to https://inframind.io/checkout/solo.
	CheckoutSoloURL = "https://inframind.io/#waitlist"
	// CheckoutTeamURL eventually points to https://inframind.io/checkout/team.
	CheckoutTeamURL = "https://inframind.io/#waitlist"
	// EnterpriseURL eventually points to https://inframind.io/enterprise
	// (Calendly or a gated enterprise form).
	EnterpriseURL = "https://inframind.io/#waitlist"
	// PartnerURL eventually points to https://inframind.io/partner
	// (partner program application form).
	PartnerURL = "https://inframind.io/#waitlist"
	// PricingURL is the public pricing page — stable across v1.0 and v1.1.
	PricingURL = "https://inframind.io/pricing"
	// WaitlistURL is the canonical fallback CTA while Cloud is in
	// early access. Do not remove — doubles as the friendly stub for
	// unsupported `--cloud aws|gcp` flags.
	WaitlistURL = "https://inframind.io/#waitlist"
)

// SoloMonthlyUSD is the numeric Solo price used for payback math in CTAs.
// Kept separate from the display string so we don't re-parse "$29/mo" at
// runtime just to compute ROI.
const SoloMonthlyUSD = 29.0

// PaybackDays returns how many days of identified monthly savings it takes
// to pay for one month of Solo. Returns 0 when savings are non-positive.
func PaybackDays(monthlySavings float64) float64 {
	if monthlySavings <= 0 {
		return 0
	}
	return 30.0 * SoloMonthlyUSD / monthlySavings
}

// ROIMultiplier returns net-savings multiple for Solo ($29/mo). Example:
// $8,234/mo identified with $29/mo Cloud → 283x ROI. Returns 0 when savings
// do not cover the subscription.
func ROIMultiplier(monthlySavings float64) float64 {
	if monthlySavings <= SoloMonthlyUSD {
		return 0
	}
	return (monthlySavings - SoloMonthlyUSD) / SoloMonthlyUSD
}
