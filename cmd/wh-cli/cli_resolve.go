package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
)

type resolvedTarget struct {
	JID         string `json:"jid"`
	Type        string `json:"type"`
	DisplayName string `json:"display_name"`
	Source      string `json:"source"`
}

type resolveOptions struct {
	addr  string
	query string
	json  bool
	kind  string
}

func runResolve(ctx context.Context, args []string) error {
	opts, err := parseResolveArgs(args)
	if err != nil {
		return err
	}
	if opts.query == "" {
		return fmt.Errorf("%w: usage resolve <name-or-jid>", errInvalidInput)
	}
	token, err := cliAccessToken(ctx)
	if err != nil {
		return err
	}
	matches, err := resolveTargets(ctx, opts.addr, token, opts.query, opts.kind)
	if err != nil {
		return err
	}
	if len(matches) == 0 {
		suggestions, suggestErr := suggestTargets(ctx, opts.addr, token, opts.query, opts.kind, 8)
		if suggestErr != nil {
			return fmt.Errorf("%w: no target found for %q", errInvalidInput, opts.query)
		}
		return noTargetError(opts.query, suggestions)
	}
	if opts.json {
		body, _ := json.Marshal(map[string]any{"items": matches})
		fmt.Println(string(body))
		return nil
	}
	printResolveTable(matches)
	return nil
}

func parseResolveArgs(args []string) (resolveOptions, error) {
	opts := resolveOptions{addr: defaultDaemonAddr, kind: "any"}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--addr":
			value, ok := nextArg(args, &i)
			if !ok {
				return resolveOptions{}, fmt.Errorf("%w: --addr requires a value", errInvalidInput)
			}
			opts.addr = value
		case "--json":
			opts.json = true
		case "--type":
			value, ok := nextArg(args, &i)
			if !ok {
				return resolveOptions{}, fmt.Errorf("%w: --type requires a value", errInvalidInput)
			}
			opts.kind = value
		default:
			if strings.HasPrefix(arg, "-") {
				return resolveOptions{}, fmt.Errorf("%w: unknown resolve option %s", errInvalidInput, arg)
			}
			if opts.query != "" {
				return resolveOptions{}, fmt.Errorf("%w: usage resolve <name-or-jid>", errInvalidInput)
			}
			opts.query = arg
		}
	}
	return opts, nil
}

func resolveSingleTarget(ctx context.Context, addr string, token string, query string, kind string) (resolvedTarget, error) {
	if isJID(query) {
		return resolvedTarget{JID: query, Type: inferTargetType(query), DisplayName: query, Source: "jid"}, nil
	}
	matches, err := resolveTargets(ctx, addr, token, query, kind)
	if err != nil {
		return resolvedTarget{}, err
	}
	if len(matches) == 0 {
		suggestions, suggestErr := suggestTargets(ctx, addr, token, query, kind, 8)
		if suggestErr != nil {
			return resolvedTarget{}, fmt.Errorf("%w: no target found for %q", errInvalidInput, query)
		}
		return resolvedTarget{}, noTargetError(query, suggestions)
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	return resolvedTarget{}, ambiguousTargetError(query, matches)
}

func suggestTargets(ctx context.Context, addr string, token string, query string, kind string, limit int) ([]resolvedTarget, error) {
	candidates, err := loadResolveCandidates(ctx, addr, token, kind)
	if err != nil {
		return nil, err
	}
	type scored struct {
		target resolvedTarget
		score  int
	}
	needle := normalizeResolveText(query)
	scoredSuggestions := make([]scored, 0)
	for _, candidate := range candidates {
		name := normalizeResolveText(candidate.DisplayName)
		score := suggestionScore(needle, name)
		if score <= 0 {
			continue
		}
		scoredSuggestions = append(scoredSuggestions, scored{target: candidate, score: score})
	}
	sort.SliceStable(scoredSuggestions, func(i, j int) bool {
		if scoredSuggestions[i].score != scoredSuggestions[j].score {
			return scoredSuggestions[i].score > scoredSuggestions[j].score
		}
		return scoredSuggestions[i].target.DisplayName < scoredSuggestions[j].target.DisplayName
	})
	if len(scoredSuggestions) > limit {
		scoredSuggestions = scoredSuggestions[:limit]
	}
	suggestions := make([]resolvedTarget, 0, len(scoredSuggestions))
	for _, suggestion := range scoredSuggestions {
		suggestions = append(suggestions, suggestion.target)
	}
	return suggestions, nil
}

func resolveTargets(ctx context.Context, addr string, token string, query string, kind string) ([]resolvedTarget, error) {
	if isJID(query) {
		return []resolvedTarget{{JID: query, Type: inferTargetType(query), DisplayName: query, Source: "jid"}}, nil
	}
	candidates, err := loadResolveCandidates(ctx, addr, token, kind)
	if err != nil {
		return nil, err
	}
	matches := rankResolveCandidates(query, candidates)
	if len(matches) > 20 {
		matches = matches[:20]
	}
	return matches, nil
}

func loadResolveCandidates(ctx context.Context, addr string, token string, kind string) ([]resolvedTarget, error) {
	seen := make(map[string]resolvedTarget)
	add := func(target resolvedTarget) {
		if target.JID == "" || isAuxiliaryChat(target.JID) {
			return
		}
		if target.DisplayName == "" {
			target.DisplayName = target.JID
		}
		if _, ok := seen[target.JID]; !ok {
			seen[target.JID] = target
		}
	}

	if kind == "any" || kind == "chat" || kind == "dm" || kind == "group" {
		body, err := httpGetAuth(ctx, strings.TrimRight(addr, "/")+"/chats?limit=200", token)
		if err != nil {
			return nil, err
		}
		var chats cliChatPage
		if err := json.Unmarshal(body, &chats); err != nil {
			return nil, fmt.Errorf("decode chats for resolve: %w", err)
		}
		for _, chat := range chats.Items {
			if kind == "dm" && chat.Type != "dm" {
				continue
			}
			if kind == "group" && chat.Type != "group" {
				continue
			}
			add(resolvedTarget{JID: chat.JID, Type: chat.Type, DisplayName: chat.DisplayName, Source: "chat"})
		}
	}

	if kind == "any" || kind == "group" {
		body, err := httpGetAuth(ctx, strings.TrimRight(addr, "/")+"/groups", token)
		if err != nil {
			return nil, err
		}
		var groups cliGroupPage
		if err := json.Unmarshal(body, &groups); err != nil {
			return nil, fmt.Errorf("decode groups for resolve: %w", err)
		}
		for _, group := range groups.Items {
			add(resolvedTarget{JID: group.JID, Type: "group", DisplayName: group.Name, Source: "group"})
		}
	}

	if kind == "any" || kind == "contact" || kind == "dm" {
		body, err := httpGetAuth(ctx, strings.TrimRight(addr, "/")+"/contacts", token)
		if err != nil {
			return nil, err
		}
		var contacts cliContactPage
		if err := json.Unmarshal(body, &contacts); err != nil {
			return nil, fmt.Errorf("decode contacts for resolve: %w", err)
		}
		for _, contact := range contacts.Items {
			add(resolvedTarget{JID: contact.JID, Type: "dm", DisplayName: contact.DisplayName, Source: "contact"})
		}
	}

	candidates := make([]resolvedTarget, 0, len(seen))
	for _, target := range seen {
		candidates = append(candidates, target)
	}
	return dedupeResolveCandidates(candidates), nil
}

func rankResolveCandidates(query string, candidates []resolvedTarget) []resolvedTarget {
	type scored struct {
		target resolvedTarget
		score  int
	}
	scoredMatches := make([]scored, 0)
	needle := normalizeResolveText(query)
	for _, candidate := range candidates {
		haystack := normalizeResolveText(candidate.DisplayName)
		score := resolveScore(needle, haystack)
		if score == 0 {
			continue
		}
		if candidate.Source == "chat" {
			score += 5
		}
		scoredMatches = append(scoredMatches, scored{target: candidate, score: score})
	}
	sort.SliceStable(scoredMatches, func(i, j int) bool {
		if scoredMatches[i].score != scoredMatches[j].score {
			return scoredMatches[i].score > scoredMatches[j].score
		}
		return scoredMatches[i].target.DisplayName < scoredMatches[j].target.DisplayName
	})
	if len(scoredMatches) == 0 {
		return nil
	}
	topScore := scoredMatches[0].score
	exactOrPrefix := topScore >= 80
	matches := make([]resolvedTarget, 0, len(scoredMatches))
	for _, match := range scoredMatches {
		if exactOrPrefix && match.score < topScore {
			break
		}
		matches = append(matches, match.target)
	}
	return matches
}

func resolveScore(needle string, haystack string) int {
	switch {
	case needle == "" || haystack == "":
		return 0
	case needle == haystack:
		return 100
	case strings.HasPrefix(haystack, needle):
		return 80
	case strings.Contains(haystack, needle):
		return 50
	default:
		return 0
	}
}

func dedupeResolveCandidates(candidates []resolvedTarget) []resolvedTarget {
	byName := make(map[string]resolvedTarget)
	others := make([]resolvedTarget, 0)
	for _, candidate := range candidates {
		key := normalizeResolveText(candidate.DisplayName)
		existing, ok := byName[key]
		if !ok {
			byName[key] = candidate
			continue
		}
		if shouldPreferResolveCandidate(candidate, existing) {
			byName[key] = candidate
			continue
		}
		if !isLegacyModernPair(candidate.JID, existing.JID) {
			others = append(others, candidate)
		}
	}
	result := make([]resolvedTarget, 0, len(byName)+len(others))
	for _, candidate := range byName {
		result = append(result, candidate)
	}
	result = append(result, others...)
	return result
}

func shouldPreferResolveCandidate(candidate resolvedTarget, existing resolvedTarget) bool {
	if candidate.Source == "chat" && existing.Source != "chat" {
		return true
	}
	if candidate.Source != "chat" && existing.Source == "chat" {
		return false
	}
	if strings.HasSuffix(candidate.JID, "@lid") && strings.HasSuffix(existing.JID, "@s.whatsapp.net") {
		return true
	}
	return false
}

func isLegacyModernPair(left string, right string) bool {
	return (strings.HasSuffix(left, "@lid") && strings.HasSuffix(right, "@s.whatsapp.net")) ||
		(strings.HasSuffix(left, "@s.whatsapp.net") && strings.HasSuffix(right, "@lid"))
}

func normalizeResolveText(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(value))), " ")
}

func ambiguousTargetError(query string, matches []resolvedTarget) error {
	var b strings.Builder
	fmt.Fprintf(&b, "%v: ambiguous target %q\n", errInvalidInput, query)
	for i, match := range matches {
		fmt.Fprintf(&b, "%d. %s [%s] %s\n", i+1, match.DisplayName, match.Type, match.JID)
	}
	fmt.Fprintf(&b, "Use the exact JID or a more specific name.")
	return fmt.Errorf("%s", strings.TrimSpace(b.String()))
}

func noTargetError(query string, suggestions []resolvedTarget) error {
	var b strings.Builder
	fmt.Fprintf(&b, "%v: no target found for %q", errInvalidInput, query)
	if len(suggestions) > 0 {
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, "Possible matches:")
		for i, suggestion := range suggestions {
			fmt.Fprintf(&b, "%d. %s [%s] %s\n", i+1, suggestion.DisplayName, suggestion.Type, suggestion.JID)
		}
		fmt.Fprintln(&b, "Use a more specific name or the exact JID.")
	}
	return fmt.Errorf("%s", strings.TrimSpace(b.String()))
}

func suggestionScore(needle string, haystack string) int {
	if needle == "" || haystack == "" {
		return 0
	}
	best := 0
	for _, token := range strings.Fields(haystack) {
		score := 60 - levenshteinDistance(needle, token)*8
		if strings.HasPrefix(token, needle[:min(len(needle), len(token))]) {
			score += 10
		}
		if score > best {
			best = score
		}
	}
	score := 60 - levenshteinDistance(needle, haystack)*4
	if score > best {
		best = score
	}
	return best
}

func levenshteinDistance(a string, b string) int {
	ar := []rune(a)
	br := []rune(b)
	if len(ar) == 0 {
		return len(br)
	}
	if len(br) == 0 {
		return len(ar)
	}
	prev := make([]int, len(br)+1)
	curr := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		curr[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 0
			if ar[i-1] != br[j-1] {
				cost = 1
			}
			curr[j] = min(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(br)]
}

func printResolveTable(matches []resolvedTarget) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "TYPE\tNAME\tSOURCE\tJID")
	for _, match := range matches {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", match.Type, truncateText(match.DisplayName, 40), match.Source, match.JID)
	}
	_ = w.Flush()
}

func isJID(value string) bool {
	value = strings.TrimSpace(value)
	return strings.Contains(value, "@") && (strings.HasSuffix(value, "@lid") ||
		strings.HasSuffix(value, "@g.us") ||
		strings.HasSuffix(value, "@s.whatsapp.net") ||
		strings.HasSuffix(value, "@newsletter") ||
		value == "status@broadcast")
}

func inferTargetType(jid string) string {
	if strings.HasSuffix(jid, "@g.us") {
		return "group"
	}
	return "dm"
}

func chatPath(addr string, jid string, suffix string) string {
	return fmt.Sprintf("%s/chats/%s%s", strings.TrimRight(addr, "/"), url.PathEscape(jid), suffix)
}
