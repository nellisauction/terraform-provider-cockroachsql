package cockroachsql

import (
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/lib/pq"
)

const (
	queryConcatKeywordWhere = "WHERE"
	queryConcatKeywordAnd   = "AND"
	queryArrayKeywordAny    = "ANY"
	queryArrayKeywordAll    = "ALL"
	likePatternQuery        = "LIKE"
	notLikePatternQuery     = "NOT LIKE"
	regexPatternQuery       = "~"
)

// queryArgs collects bind parameters so user-supplied filter values never
// become part of the SQL text.
type queryArgs struct {
	args []any
}

func (q *queryArgs) add(v any) string {
	q.args = append(q.args, v)
	return fmt.Sprintf("$%d", len(q.args))
}

func toStringSlice(values []any) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = v.(string)
	}
	return out
}

func applyPatternMatchingToQuery(patternMatchingTarget string, d *schema.ResourceData, q *queryArgs) []string {
	likeAnyPatterns := d.Get("like_any_patterns").([]any)
	likeAllPatterns := d.Get("like_all_patterns").([]any)
	notLikeAllPatterns := d.Get("not_like_all_patterns").([]any)
	regexPattern := d.Get("regex_pattern").(string)

	filters := []string{}
	if len(likeAnyPatterns) > 0 {
		filters = append(filters, generatePatternMatchingString(patternMatchingTarget, likePatternQuery, generatePatternArrayParam(likeAnyPatterns, queryArrayKeywordAny, q)))
	}
	if len(likeAllPatterns) > 0 {
		filters = append(filters, generatePatternMatchingString(patternMatchingTarget, likePatternQuery, generatePatternArrayParam(likeAllPatterns, queryArrayKeywordAll, q)))
	}
	if len(notLikeAllPatterns) > 0 {
		filters = append(filters, generatePatternMatchingString(patternMatchingTarget, notLikePatternQuery, generatePatternArrayParam(notLikeAllPatterns, queryArrayKeywordAll, q)))
	}
	if regexPattern != "" {
		filters = append(filters, generatePatternMatchingString(patternMatchingTarget, regexPatternQuery, q.add(regexPattern)))
	}

	return filters
}

func generatePatternMatchingString(patternMatchingTarget string, additionalQueryKeyword string, pattern string) string {
	patternMatchingFilter := fmt.Sprintf("%s %s %s", patternMatchingTarget, additionalQueryKeyword, pattern)

	return patternMatchingFilter
}

func applyTypeMatchingToQuery(objectKeyword string, objects []any, q *queryArgs) string {
	var typeFilter string
	if len(objects) > 0 {
		typeFilter = fmt.Sprintf("%s = %s", objectKeyword, generatePatternArrayParam(objects, queryArrayKeywordAny, q))
	}

	return typeFilter
}

func generatePatternArrayParam(patterns []any, queryArrayKeyword string, q *queryArgs) string {
	return fmt.Sprintf("%s (%s::STRING[])", queryArrayKeyword, q.add(pq.Array(toStringSlice(patterns))))
}

// generatePatternArrayString renders patterns for data source IDs only; it
// must never be used to build SQL.
func generatePatternArrayString(patterns []any, queryArrayKeyword string) string {
	formattedPatterns := []string{}

	for _, pattern := range patterns {
		formattedPatterns = append(formattedPatterns, fmt.Sprintf("'%s'", pattern.(string)))
	}
	return fmt.Sprintf("%s (array[%s])", queryArrayKeyword, strings.Join(formattedPatterns, ","))
}

func finalizeQueryWithFilters(query string, queryConcatKeyword string, filters []string) string {
	if len(filters) > 0 {
		query = fmt.Sprintf("%s %s %s", query, queryConcatKeyword, strings.Join(filters, " AND "))
	}

	return query
}
