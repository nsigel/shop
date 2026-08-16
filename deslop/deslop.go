package deslop

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/nsigel/deslop"
	"golang.org/x/text/unicode/norm"
)

type Level string

const (
	LevelRelaxed  Level = "relaxed"
	LevelStandard Level = "standard"
	LevelStrict   Level = "strict"
)

type Verdict string

const (
	VerdictAllowed   Verdict = "allowed"
	VerdictBlocked   Verdict = "blocked"
	VerdictFlagged   Verdict = "flagged"
	VerdictKnown     Verdict = "known"
	VerdictSuspect   Verdict = "suspect"
	VerdictUnknown   Verdict = "unknown"
	VerdictUnbranded Verdict = "unbranded"
	VerdictForeign   Verdict = "foreign"
)

type Options struct {
	Level            Level    `json:"level"`
	FlagChineseMajor bool     `json:"flagChineseMajor,omitempty"`
	Allow            []string `json:"allow,omitempty"`
	Block            []string `json:"block,omitempty"`
}

type VerdictInfo struct {
	Verdict Verdict `json:"verdict"`
	Reason  string  `json:"reason"`
	Brand   string  `json:"brand,omitempty"`
	Key     string  `json:"key,omitempty"`
	Score   int     `json:"score,omitempty"`
}

type Evaluation struct {
	Level    Level        `json:"level"`
	Filtered bool         `json:"filtered"`
	Product  VerdictInfo  `json:"product"`
	Seller   *VerdictInfo `json:"seller,omitempty"`
}

type SearchResult struct {
	Result         *shop.SearchResult `json:"result"`
	Filtered       int                `json:"filtered"`
	FilteredBy     map[Verdict]int    `json:"filteredBy,omitempty"`
	LookupFailures int                `json:"lookupFailures,omitempty"`
}

type scoreResult struct {
	score   int
	reasons []string
}

type classifierIndex struct {
	known        map[string]string
	flagged      map[string]string
	chineseMajor map[string]string
	generic      map[string]struct{}
}

var (
	idx = classifierIndex{
		known:        map[string]string{},
		flagged:      map[string]string{},
		chineseMajor: map[string]string{},
		generic:      map[string]struct{}{},
	}

	compatBaitWords = setOf(
		"apple", "iphone", "ipad", "ipod", "macbook", "airpods",
		"samsung", "galaxy",
	)
	compatMarkers = setOf(
		"compatible", "for", "fits", "fit", "with", "works", "support", "supports",
	)
	sellerNoise = setOf(
		"co", "ltd", "inc", "llc", "limited", "company", "corp", "gmbh",
		"store", "shop", "shops", "mall", "outlet", "retail", "market",
		"direct", "official", "authorized", "flagship", "online", "global",
		"international", "trading", "trade", "technology", "tech", "group",
		"industry", "industries", "supply", "supplies", "service", "services",
		"seller", "sales", "warehouse", "depot", "express", "home", "life",
		"us", "usa", "uk", "eu", "ca", "de", "fr", "jp", "na", "the", "and",
	)
	nonWordSplit = regexp.MustCompile(`[^A-Za-z0-9]+`)
)

func init() {
	for _, brand := range knownBrands {
		addKnown(brand)
	}
	for _, brand := range communityBrands {
		addKnown(brand)
	}
	for _, brand := range chineseMajorBrands {
		key := normalize(brand)
		if key == "" {
			continue
		}
		if _, ok := idx.chineseMajor[key]; !ok {
			idx.chineseMajor[key] = brand
		}
		addKnown(brand)
	}
	for _, brand := range flaggedBrands {
		key := normalize(brand)
		if key == "" {
			continue
		}
		if _, ok := idx.flagged[key]; !ok {
			idx.flagged[key] = brand
		}
	}
	for _, word := range genericWords {
		key := normalize(word)
		if key != "" {
			idx.generic[key] = struct{}{}
		}
	}
}

func DefaultOptions() Options {
	return Options{Level: LevelStandard}
}

func EvaluateProduct(product *shop.Product, opts Options) Evaluation {
	opts = opts.withDefaults()

	productVerdict := classifyBrand(product.Brand, product.Title, opts)
	eval := Evaluation{
		Level:    opts.Level,
		Filtered: ShouldAct(productVerdict.Verdict, opts.Level),
		Product:  productVerdict,
	}

	if product.Seller != nil && strings.TrimSpace(product.Seller.Name) != "" {
		sellerVerdict := classifySeller(product.Seller.Name, opts)
		eval.Seller = &sellerVerdict
	}

	return eval
}

func FilterSearch(ctx context.Context, store shop.Store, result *shop.SearchResult, opts Options) (*SearchResult, error) {
	opts = opts.withDefaults()

	filteredBy := map[Verdict]int{}
	warnings := append([]string{}, result.Warnings...)
	kept := make([]shop.ProductSummary, 0, len(result.Products))
	lookupFailures := 0

	for _, summary := range result.Products {
		product, err := store.Product(ctx, summary.ID)
		if err != nil {
			lookupFailures++
			warnings = append(warnings, fmt.Sprintf("deslop lookup failed for %s: %v", summary.ID, err))
			kept = append(kept, summary)
			continue
		}

		eval := EvaluateProduct(product, opts)
		summary.Brand = product.Brand
		if summary.Rating == nil {
			summary.Rating = product.Rating
		}
		if summary.Attributes == nil {
			summary.Attributes = map[string]any{}
		}
		summary.Attributes["deslop"] = eval

		if eval.Filtered {
			filteredBy[eval.Product.Verdict]++
			continue
		}

		kept = append(kept, summary)
	}

	totalFiltered := 0
	for _, count := range filteredBy {
		totalFiltered += count
	}
	if totalFiltered > 0 {
		warnings = append(warnings, fmt.Sprintf("deslop filtered %d result(s) (%s)", totalFiltered, formatVerdictCounts(filteredBy)))
	}

	cloned := *result
	cloned.Products = kept
	cloned.Count = len(kept)
	cloned.Warnings = warnings

	return &SearchResult{
		Result:         &cloned,
		Filtered:       totalFiltered,
		FilteredBy:     filteredBy,
		LookupFailures: lookupFailures,
	}, nil
}

func ShouldAct(verdict Verdict, level Level) bool {
	level = withDefaultLevel(level)

	switch level {
	case LevelRelaxed:
		return verdict == VerdictBlocked || verdict == VerdictFlagged
	case LevelStrict:
		return verdict == VerdictBlocked || verdict == VerdictFlagged || verdict == VerdictSuspect || verdict == VerdictUnbranded || verdict == VerdictUnknown
	default:
		return verdict == VerdictBlocked || verdict == VerdictFlagged || verdict == VerdictSuspect || verdict == VerdictUnbranded
	}
}

func classifyBrand(brandText, title string, opts Options) VerdictInfo {
	name := strings.TrimSpace(brandText)
	if name == "" {
		return VerdictInfo{
			Verdict: VerdictUnbranded,
			Reason:  "no brand returned by the store",
		}
	}

	key := normalize(name)
	if key == "" {
		return VerdictInfo{
			Verdict: VerdictForeign,
			Reason:  "brand isn't in a script deslop can read yet",
			Brand:   name,
		}
	}

	return verdictFor(name, key, title, opts)
}

func classifySeller(name string, opts Options) VerdictInfo {
	key := normalize(name)
	if key == "" {
		return VerdictInfo{Verdict: VerdictUnknown, Reason: "no readable seller name", Brand: strings.TrimSpace(name)}
	}

	allow := normalizeSet(opts.Allow)
	block := normalizeSet(opts.Block)
	result := VerdictInfo{Brand: strings.TrimSpace(name), Key: key}

	if _, ok := allow[key]; ok {
		result.Verdict = VerdictAllowed
		result.Reason = "seller is on your allowlist"
		return result
	}
	if _, ok := block[key]; ok {
		result.Verdict = VerdictBlocked
		result.Reason = "seller is on your blocklist"
		return result
	}
	if _, ok := idx.flagged[key]; ok {
		result.Verdict = VerdictFlagged
		result.Reason = "seller name is on the known pseudo-brand list"
		return result
	}
	if _, ok := idx.known[key]; ok {
		result.Verdict = VerdictKnown
		result.Reason = "storefront of an established brand"
		return result
	}

	best := scoreResult{}
	for _, token := range strings.Fields(name) {
		tokenKey := normalize(token)
		if tokenKey == "" {
			continue
		}
		if _, ok := sellerNoise[tokenKey]; ok {
			continue
		}
		if _, ok := idx.flagged[tokenKey]; ok {
			result.Verdict = VerdictFlagged
			result.Reason = "seller name contains a listed pseudo-brand"
			return result
		}
		if _, ok := idx.known[tokenKey]; ok {
			result.Verdict = VerdictKnown
			result.Reason = "storefront of an established brand"
			return result
		}
		if _, ok := allow[tokenKey]; ok {
			result.Verdict = VerdictKnown
			result.Reason = "storefront of an allowlisted brand"
			return result
		}
		if _, ok := block[tokenKey]; ok {
			result.Verdict = VerdictFlagged
			result.Reason = "seller name contains a blocklisted pseudo-brand"
			return result
		}

		score := scoreBrand(token)
		if score.score > best.score {
			best = score
		}
	}

	result.Score = best.score
	if best.score >= 6 {
		result.Verdict = VerdictFlagged
		result.Reason = "seller name looks like a pseudo-brand: " + strings.Join(best.reasons, ", ")
		return result
	}

	result.Verdict = VerdictUnknown
	result.Reason = "seller not on any list"
	return result
}

func verdictFor(name, key, title string, opts Options) VerdictInfo {
	allow := normalizeSet(opts.Allow)
	block := normalizeSet(opts.Block)
	result := VerdictInfo{
		Brand: name,
		Key:   key,
	}

	if _, ok := allow[key]; ok {
		result.Verdict = VerdictAllowed
		result.Reason = "on your allowlist"
		return result
	}
	if _, ok := block[key]; ok {
		result.Verdict = VerdictBlocked
		result.Reason = "on your blocklist"
		return result
	}
	if _, ok := idx.flagged[key]; ok {
		result.Verdict = VerdictFlagged
		result.Reason = "on the known pseudo-brand list"
		return result
	}
	if _, ok := idx.chineseMajor[key]; ok {
		if opts.FlagChineseMajor {
			result.Verdict = VerdictFlagged
			result.Reason = "established Chinese brand (flagged by your settings)"
		} else {
			result.Verdict = VerdictKnown
			result.Reason = "established brand (Chinese-owned)"
		}
		return result
	}
	if _, ok := idx.known[key]; ok {
		result.Verdict = VerdictKnown
		result.Reason = "established brand"
		return result
	}

	score := scoreBrand(name)
	bait := compatBait(title, key)
	if bait != "" && score.score < 6 {
		score.score = min(score.score+3, 5)
		score.reasons = append(score.reasons, "name-drops "+bait+" for compatibility")
	}

	result.Score = score.score
	switch {
	case score.score >= 6:
		result.Verdict = VerdictFlagged
		result.Reason = "looks like a pseudo-brand: " + strings.Join(score.reasons, ", ")
	case score.score >= 3:
		result.Verdict = VerdictSuspect
		result.Reason = "unrecognized brand: " + strings.Join(score.reasons, ", ")
	default:
		result.Verdict = VerdictUnknown
		result.Reason = "brand not on any list"
	}

	return result
}

func scoreBrand(name string) scoreResult {
	score := scoreResult{}
	letters := lettersOnly(name)
	if letters == "" {
		return score
	}

	if hasNonLatinLetter(name) {
		score.score += 4
		score.reasons = append(score.reasons, "non-Latin characters")
	}

	isAllCaps := len(letters) >= 3 && letters == strings.ToUpper(letters)
	if isAllCaps {
		score.score += 3
		score.reasons = append(score.reasons, "all-caps name")
		if len(letters) >= 5 && len(letters) <= 9 {
			score.score++
			score.reasons = append(score.reasons, "typical squat-name length")
		}
	}

	vowels := 0
	for _, r := range letters {
		if strings.ContainsRune("aeiouyAEIOUY", r) {
			vowels++
		}
	}
	ratio := float64(vowels) / float64(len(letters))
	switch {
	case ratio < 0.18:
		score.score += 3
		score.reasons = append(score.reasons, "almost no vowels")
	case ratio < 0.28:
		score.score++
		score.reasons = append(score.reasons, "few vowels")
	case ratio > 0.62:
		score.score++
		score.reasons = append(score.reasons, "mostly vowels")
	}

	if hasConsonantRun(insertCaseSeams(letters)) {
		score.score += 3
		score.reasons = append(score.reasons, "unpronounceable consonant run")
	}

	if hasUnEnglishPair(letters) {
		score.score += 2
		score.reasons = append(score.reasons, "un-English letter pairs")
	}

	if hasEmbeddedDigit(name) {
		score.score++
		score.reasons = append(score.reasons, "digits inside name")
	}

	if countCaseFlips(name) >= 2 {
		score.score += 2
		score.reasons = append(score.reasons, "random capitalization")
	}

	return score
}

func compatBait(title, brandKey string) string {
	words := nonWordSplit.Split(title, -1)
	for i, word := range words {
		if word == "" {
			continue
		}
		key := strings.ToLower(word)
		if _, ok := compatBaitWords[key]; !ok || key == brandKey {
			continue
		}
		if hasCompatMarker(words, i) {
			return word
		}
	}
	return ""
}

func hasCompatMarker(words []string, index int) bool {
	start := max(0, index-3)
	for i := start; i < index; i++ {
		if _, ok := compatMarkers[strings.ToLower(words[i])]; ok {
			return true
		}
	}
	return false
}

func lettersOnly(name string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(name) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		if ('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func normalize(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(strings.ToLower(s)) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		if ('a' <= r && r <= 'z') || ('0' <= r && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func normalizeSet(items []string) map[string]struct{} {
	out := make(map[string]struct{}, len(items))
	for _, item := range items {
		if key := normalize(item); key != "" {
			out[key] = struct{}{}
		}
	}
	return out
}

func hasNonLatinLetter(name string) bool {
	for _, r := range name {
		if !unicode.IsLetter(r) {
			continue
		}
		if !unicode.In(r, unicode.Latin) {
			return true
		}
	}
	return false
}

func insertCaseSeams(s string) string {
	var b strings.Builder
	var prev rune
	for i, r := range s {
		if i > 0 && unicode.IsLower(prev) && unicode.IsUpper(r) {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
		prev = r
	}
	return b.String()
}

func hasConsonantRun(s string) bool {
	run := 0
	for _, r := range s {
		if strings.ContainsRune("bcdfghjklmnpqrstvwxzBCDFGHJKLMNPQRSTVWXZ", r) {
			run++
			if run >= 4 {
				return true
			}
			continue
		}
		run = 0
	}
	return false
}

func hasEmbeddedDigit(name string) bool {
	for i, r := range name {
		if unicode.IsDigit(r) && i > 0 {
			return true
		}
	}
	return false
}

func countCaseFlips(name string) int {
	flips := 0
	var prev rune
	havePrev := false
	for _, r := range name {
		if !unicode.IsLetter(r) {
			havePrev = false
			continue
		}
		if havePrev && unicode.IsLower(prev) && unicode.IsUpper(r) {
			flips++
		}
		prev = r
		havePrev = true
	}
	return flips
}

func hasUnEnglishPair(s string) bool {
	lower := strings.ToLower(s)
	for i := 0; i < len(lower); i++ {
		if lower[i] == 'q' && (i+1 >= len(lower) || lower[i+1] != 'u') {
			return true
		}
	}

	pairs := []string{"jx", "vx", "wx", "xx", "xj", "xk", "xq", "xz", "zx", "zj", "zq", "bz", "cz", "dz", "fz", "gz", "pz"}
	for _, pair := range pairs {
		if strings.Contains(lower, pair) {
			return true
		}
	}

	return false
}

func formatVerdictCounts(counts map[Verdict]int) string {
	keys := make([]string, 0, len(counts))
	for verdict, count := range counts {
		keys = append(keys, fmt.Sprintf("%s:%d", verdict, count))
	}
	slices.Sort(keys)
	return strings.Join(keys, ", ")
}

func addKnown(brand string) {
	key := normalize(brand)
	if key == "" {
		return
	}
	if _, ok := idx.known[key]; !ok {
		idx.known[key] = brand
	}
}

func setOf(items ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(items))
	for _, item := range items {
		out[item] = struct{}{}
	}
	return out
}

func (o Options) withDefaults() Options {
	o.Level = withDefaultLevel(o.Level)
	return o
}

func withDefaultLevel(level Level) Level {
	switch level {
	case LevelRelaxed, LevelStandard, LevelStrict:
		return level
	default:
		return LevelStandard
	}
}
