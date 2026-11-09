package leetcode

// GraphQL queries (POST /graphql/). The meta queries never name the problem
// body, its code snippets or examples, so a premium problem's body is never
// requested (A10.6).

const metaFields = `questionId questionFrontendId titleSlug title difficulty isPaidOnly topicTags { name slug }`

const dailyQuery = `query questionOfToday { activeDailyCodingChallengeQuestion { date question { ` + metaFields + ` } } }`

const questionMetaQuery = `query questionMeta($titleSlug: String!) { question(titleSlug: $titleSlug) { ` + metaFields + ` } }`

const questionBodyQuery = `query questionData($titleSlug: String!) { question(titleSlug: $titleSlug) { ` + metaFields +
	` content codeSnippets { lang langSlug code } exampleTestcaseList } }`

const userStatusQuery = `query globalData { userStatus { isSignedIn username isPremium } }`

// gqlRequest is the POST /graphql/ body.
type gqlRequest struct {
	OperationName string         `json:"operationName"`
	Query         string         `json:"query"`
	Variables     map[string]any `json:"variables"`
}

// gqlError is one GraphQL error.
type gqlError struct {
	Message string `json:"message"`
}

// gqlQuestion is the question object as LeetCode sends it.
type gqlQuestion struct {
	QuestionID         string `json:"questionId"`
	QuestionFrontendID string `json:"questionFrontendId"`
	TitleSlug          string `json:"titleSlug"`
	Title              string `json:"title"`
	Difficulty         string `json:"difficulty"`
	IsPaidOnly         bool   `json:"isPaidOnly"`
	TopicTags          []struct {
		Name string `json:"name"`
	} `json:"topicTags"`
	Content      *string `json:"content"`
	CodeSnippets []struct {
		LangSlug string `json:"langSlug"`
		Code     string `json:"code"`
	} `json:"codeSnippets"`
	ExampleTestcaseList []string `json:"exampleTestcaseList"`
}

func (g *gqlQuestion) question() *Question {
	q := &Question{
		QuestionID: g.QuestionID, FrontendID: g.QuestionFrontendID, Slug: g.TitleSlug, Title: g.Title,
		Difficulty: g.Difficulty, IsPaidOnly: g.IsPaidOnly, Tags: []string{}, Snippets: map[string]string{},
		ExampleTestcases: append([]string{}, g.ExampleTestcaseList...),
	}
	for _, t := range g.TopicTags {
		q.Tags = append(q.Tags, t.Name)
	}
	if g.Content != nil {
		q.ContentHTML = *g.Content
	}
	for _, s := range g.CodeSnippets {
		q.Snippets[s.LangSlug] = s.Code
	}
	return q
}
