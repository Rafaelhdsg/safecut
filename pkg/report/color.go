package report

import (
	"fmt"
	"os"

	"golang.org/x/term"

	"github.com/Rafaelhdsg/inframind-cli/internal/pricing_tiers"
)

var colorEnabled = detectTerminal()

func detectTerminal() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("FORCE_COLOR") != "" {
		return true
	}
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// SetColor forces color on or off. Wired from the persistent --no-color flag
// in cmd/root.go, and also from tests that need deterministic output.
func SetColor(enabled bool) {
	colorEnabled = enabled
}

const (
	reset  = "\033[0m"
	bold   = "\033[1m"
	dim    = "\033[2m"
	red    = "\033[31m"
	green  = "\033[32m"
	yellow = "\033[33m"
	cyan   = "\033[36m"
	white  = "\033[37m"

	boldRed    = "\033[1;31m"
	boldGreen  = "\033[1;32m"
	boldYellow = "\033[1;33m"
	boldCyan   = "\033[1;36m"
	boldWhite  = "\033[1;37m"

	bgRed    = "\033[41m"
	bgYellow = "\033[43m"
)

func c(code, text string) string {
	if !colorEnabled {
		return text
	}
	return code + text + reset
}

// Semantic color functions — used across all reports for visual consistency.

func Red(s string) string        { return c(red, s) }
func Green(s string) string      { return c(green, s) }
func Yellow(s string) string     { return c(yellow, s) }
func Cyan(s string) string       { return c(cyan, s) }
func Bold(s string) string       { return c(bold, s) }
func Dim(s string) string        { return c(dim, s) }
func BoldRed(s string) string    { return c(boldRed, s) }
func BoldGreen(s string) string  { return c(boldGreen, s) }
func BoldYellow(s string) string { return c(boldYellow, s) }
func BoldCyan(s string) string   { return c(boldCyan, s) }
func BoldWhite(s string) string  { return c(boldWhite, s) }

func BadgeCritical(s string) string {
	if !colorEnabled {
		return "[CRITICAL] " + s
	}
	return bgRed + boldWhite + " CRITICAL " + reset + " " + BoldRed(s)
}

func BadgeHigh(s string) string {
	if !colorEnabled {
		return "[HIGH] " + s
	}
	return boldRed + "[HIGH]" + reset + " " + Red(s)
}

func BadgeMedium(s string) string {
	if !colorEnabled {
		return "[MEDIUM] " + s
	}
	return boldYellow + "[MEDIUM]" + reset + " " + Yellow(s)
}

func BadgeLow(s string) string {
	if !colorEnabled {
		return "[LOW] " + s
	}
	return boldGreen + "[LOW]" + reset + " " + Green(s)
}

// Money formats a plain monetary amount (no coloring) with an explicit
// "USD" suffix. Use this for single-amount displays in tables, reports,
// and markdown exports where we don't want color codes.
func Money(amount float64) string {
	return fmt.Sprintf("$%.2f USD", amount)
}

// Money0 is Money with zero decimal places, for headline numbers where
// cents would be visual noise (e.g. "$12,345 USD/yr").
func Money0(amount float64) string {
	return fmt.Sprintf("$%.0f USD", amount)
}

// Savings formats a monetary amount with an explicit USD suffix.
// Every dollar figure the CLI prints is in USD because the Azure Retail
// Prices API is queried with currencyCode=USD. Making the currency
// explicit in the output removes any ambiguity for users in non-USD
// regions (EU, LATAM, APAC) who might otherwise assume local currency.
func Savings(amount float64) string {
	s := fmt.Sprintf("$%.2f USD", amount)
	if amount > 0 {
		return BoldGreen(s)
	}
	return s
}

func SavingsDelta(delta float64) string {
	if delta > 0 {
		return BoldGreen(fmt.Sprintf("+$%.2f USD", delta))
	}
	if delta < 0 {
		return BoldRed(fmt.Sprintf("-$%.2f USD", -delta))
	}
	return Dim("$0.00 USD")
}

func RiskColor(risk string) string {
	switch risk {
	case "high":
		return BoldRed(risk)
	case "medium":
		return Yellow(risk)
	default:
		return Green(risk)
	}
}

func Header(s string) string  { return BoldCyan(s) }
func Section(s string) string { return BoldWhite(s) }

// WaitlistURL is kept as a fallback CTA endpoint. New code should prefer
// pricing_tiers.CheckoutSoloURL / PricingURL for conversion-focused links.
const WaitlistURL = pricing_tiers.WaitlistURL

// CloudCTA returns the terminal conversion footer tuned to the actual
// savings identified in the current scan. When monthlySavings > $29 we
// surface a concrete payback in days and link to the Solo founding
// waitlist. Otherwise we fall back to the pricing comparison page. Call
// with 0 when you don't have savings context (e.g. policy-sim footer).
//
// Copy intentionally says "lock in founding price" rather than "start
// trial": InfraMind Cloud checkout is not live until v1.1, and the
// waitlist is the honest CTA while the product catches up to the pitch.
func CloudCTA(monthlySavings float64) string {
	if monthlySavings >= pricing_tiers.SoloMonthlyUSD {
		payback := pricing_tiers.PaybackDays(monthlySavings)
		return Dim("  You have ") +
			Savings(monthlySavings) +
			Dim("/mo in safe recommendations. ") +
			Bold(fmt.Sprintf("Solo $%.0f/mo would pay back in %.1f days.", pricing_tiers.SoloMonthlyUSD, payback)) +
			"\n" +
			Dim("  Lock in founding price → ") +
			Cyan(pricing_tiers.CheckoutSoloURL)
	}
	return Dim("  Compare plans & join the waitlist → ") +
		Cyan(pricing_tiers.PricingURL)
}
