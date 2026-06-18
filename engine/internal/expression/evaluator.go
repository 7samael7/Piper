package expression

import (
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/7samael7/Piper/engine/internal/pipeline/model"
)

type Context struct {
	Values map[string]interface{}
	Status model.RunStatus
}

type tokenKind int

const (
	tokenEOF tokenKind = iota
	tokenIdentifier
	tokenString
	tokenNumber
	tokenLParen
	tokenRParen
	tokenComma
	tokenNot
	tokenAnd
	tokenOr
	tokenEqual
	tokenNotEqual
)

type token struct {
	kind tokenKind
	text string
	pos  int
}

type parser struct {
	tokens       []token
	index        int
	ctx          Context
	provider     model.ProviderID
	allowUnknown bool
	skip         bool
}

func Evaluate(spec model.ConditionSpec, ctx Context) model.ConditionResult {
	result := model.ConditionResult{Expression: spec.Original, Evaluated: true}
	source := normalize(spec.Provider, spec.Original)
	if strings.TrimSpace(source) == "" {
		result.Value = true
		result.Reason = "No condition was specified."
		return result
	}
	tokens, err := lex(source)
	if err != nil {
		result.Error = err
		result.Reason = err.Message
		return result
	}
	p := &parser{tokens: tokens, ctx: ctx, provider: spec.Provider}
	value, evalErr := p.parseExpression()
	if evalErr == nil && p.peek().kind != tokenEOF {
		evalErr = evaluationError("expression.trailing_tokens", "unexpected token "+p.peek().text, p.peek().pos)
	}
	if evalErr != nil {
		result.Error = evalErr
		result.Reason = evalErr.Message
		return result
	}
	result.Value = truthy(value)
	if result.Value {
		result.Reason = "Condition evaluated to true."
	} else {
		result.Reason = "Condition evaluated to false."
	}
	return result
}

var interpolationPattern = regexp.MustCompile(`\$\{\{\s*(.*?)\s*\}\}`)
var azureMacroPattern = regexp.MustCompile(`\$\(([A-Za-z_][A-Za-z0-9_.-]*)\)`)
var azureVariableIndexPattern = regexp.MustCompile(`variables\[['"]([^'"]+)['"]\]`)
var gitlabRegexPattern = regexp.MustCompile(`\$([A-Za-z_][A-Za-z0-9_]*)\s*(=~|!~)\s*/([^/]+)/`)

func Interpolate(provider model.ProviderID, input string, ctx Context) (string, *model.EvaluationError) {
	var firstErr *model.EvaluationError
	output := interpolationPattern.ReplaceAllStringFunc(input, func(match string) string {
		if firstErr != nil {
			return match
		}
		parts := interpolationPattern.FindStringSubmatch(match)
		if len(parts) != 2 {
			return match
		}
		value, err := evaluateValue(provider, parts[1], ctx)
		if err != nil {
			firstErr = err
			return match
		}
		return stringify(value)
	})
	if provider == model.ProviderAzure && firstErr == nil {
		output = azureMacroPattern.ReplaceAllStringFunc(output, func(match string) string {
			parts := azureMacroPattern.FindStringSubmatch(match)
			if len(parts) != 2 {
				return match
			}
			if value, ok := resolve(ctx.Values, "variables."+parts[1]); ok {
				return stringify(value)
			}
			return match
		})
	}
	return output, firstErr
}

func Validate(spec model.ConditionSpec) *model.EvaluationError {
	source := normalize(spec.Provider, spec.Original)
	if strings.TrimSpace(source) == "" {
		return nil
	}
	tokens, err := lex(source)
	if err != nil {
		return err
	}
	p := &parser{tokens: tokens, ctx: Context{Values: map[string]interface{}{}}, provider: spec.Provider, allowUnknown: true}
	_, parseErr := p.parseExpression()
	if parseErr != nil {
		return parseErr
	}
	if p.peek().kind != tokenEOF {
		return evaluationError("expression.trailing_tokens", "unexpected token "+p.peek().text, p.peek().pos)
	}
	return nil
}

func evaluateValue(provider model.ProviderID, source string, ctx Context) (interface{}, *model.EvaluationError) {
	tokens, err := lex(normalize(provider, source))
	if err != nil {
		return nil, err
	}
	p := &parser{tokens: tokens, ctx: ctx, provider: provider}
	value, evalErr := p.parseExpression()
	if evalErr != nil {
		return nil, evalErr
	}
	if p.peek().kind != tokenEOF {
		return nil, evaluationError("expression.trailing_tokens", "unexpected token "+p.peek().text, p.peek().pos)
	}
	return value, nil
}

func normalize(provider model.ProviderID, source string) string {
	source = strings.TrimSpace(source)
	if strings.HasPrefix(source, "${{") && strings.HasSuffix(source, "}}") {
		source = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(source, "${{"), "}}"))
	}
	if provider == model.ProviderGitLab {
		source = gitlabRegexPattern.ReplaceAllStringFunc(source, func(match string) string {
			parts := gitlabRegexPattern.FindStringSubmatch(match)
			call := fmt.Sprintf("regex(env.%s, %q)", parts[1], parts[3])
			if parts[2] == "!~" {
				return "!" + call
			}
			return call
		})
		source = regexp.MustCompile(`\$([A-Za-z_][A-Za-z0-9_]*)`).ReplaceAllString(source, "env.$1")
		source = strings.ReplaceAll(source, " and ", " && ")
		source = strings.ReplaceAll(source, " or ", " || ")
	}
	if provider == model.ProviderAzure {
		source = azureVariableIndexPattern.ReplaceAllString(source, "get(variables, '$1')")
	}
	return source
}

func lex(source string) ([]token, *model.EvaluationError) {
	tokens := []token{}
	for pos := 0; pos < len(source); {
		r := rune(source[pos])
		if unicode.IsSpace(r) {
			pos++
			continue
		}
		start := pos
		switch {
		case strings.HasPrefix(source[pos:], "&&"):
			tokens = append(tokens, token{kind: tokenAnd, text: "&&", pos: pos})
			pos += 2
		case strings.HasPrefix(source[pos:], "||"):
			tokens = append(tokens, token{kind: tokenOr, text: "||", pos: pos})
			pos += 2
		case strings.HasPrefix(source[pos:], "=="):
			tokens = append(tokens, token{kind: tokenEqual, text: "==", pos: pos})
			pos += 2
		case strings.HasPrefix(source[pos:], "!="):
			tokens = append(tokens, token{kind: tokenNotEqual, text: "!=", pos: pos})
			pos += 2
		case source[pos] == '!':
			tokens = append(tokens, token{kind: tokenNot, text: "!", pos: pos})
			pos++
		case source[pos] == '(':
			tokens = append(tokens, token{kind: tokenLParen, text: "(", pos: pos})
			pos++
		case source[pos] == ')':
			tokens = append(tokens, token{kind: tokenRParen, text: ")", pos: pos})
			pos++
		case source[pos] == ',':
			tokens = append(tokens, token{kind: tokenComma, text: ",", pos: pos})
			pos++
		case source[pos] == '\'' || source[pos] == '"':
			quote := source[pos]
			pos++
			var value strings.Builder
			for pos < len(source) && source[pos] != quote {
				if source[pos] == '\\' && pos+1 < len(source) {
					pos++
				}
				value.WriteByte(source[pos])
				pos++
			}
			if pos >= len(source) {
				return nil, evaluationError("expression.unterminated_string", "unterminated string literal", start)
			}
			pos++
			tokens = append(tokens, token{kind: tokenString, text: value.String(), pos: start})
		case unicode.IsDigit(r):
			for pos < len(source) && (unicode.IsDigit(rune(source[pos])) || source[pos] == '.') {
				pos++
			}
			tokens = append(tokens, token{kind: tokenNumber, text: source[start:pos], pos: start})
		case unicode.IsLetter(r) || source[pos] == '_' || source[pos] == '.':
			for pos < len(source) {
				ch := rune(source[pos])
				if !(unicode.IsLetter(ch) || unicode.IsDigit(ch) || source[pos] == '_' || source[pos] == '-' || source[pos] == '.') {
					break
				}
				pos++
			}
			tokens = append(tokens, token{kind: tokenIdentifier, text: source[start:pos], pos: start})
		default:
			return nil, evaluationError("expression.invalid_character", fmt.Sprintf("unexpected character %q", source[pos]), pos)
		}
	}
	tokens = append(tokens, token{kind: tokenEOF, pos: len(source)})
	return tokens, nil
}

func (p *parser) parseExpression() (interface{}, *model.EvaluationError) {
	return p.parseOr()
}

func (p *parser) parseOr() (interface{}, *model.EvaluationError) {
	left, err := p.parseAnd()
	for err == nil && p.match(tokenOr) {
		if !p.skip && truthy(left) {
			err = p.parseSkipped(p.parseAnd)
			continue
		}
		var right interface{}
		right, err = p.parseAnd()
		if err == nil && !truthy(left) {
			left = right
		}
	}
	return left, err
}

func (p *parser) parseAnd() (interface{}, *model.EvaluationError) {
	left, err := p.parseEquality()
	for err == nil && p.match(tokenAnd) {
		if !p.skip && !truthy(left) {
			err = p.parseSkipped(p.parseEquality)
			left = false
			continue
		}
		var right interface{}
		right, err = p.parseEquality()
		if err == nil && truthy(left) {
			left = right
		}
	}
	return left, err
}

func (p *parser) parseSkipped(parse func() (interface{}, *model.EvaluationError)) *model.EvaluationError {
	previous := p.skip
	p.skip = true
	_, err := parse()
	p.skip = previous
	return err
}

func (p *parser) parseEquality() (interface{}, *model.EvaluationError) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tokenEqual || p.peek().kind == tokenNotEqual {
		operator := p.advance()
		right, rightErr := p.parseUnary()
		if rightErr != nil {
			return nil, rightErr
		}
		equal := equals(left, right)
		if operator.kind == tokenNotEqual {
			equal = !equal
		}
		left = equal
	}
	return left, nil
}

func (p *parser) parseUnary() (interface{}, *model.EvaluationError) {
	if p.match(tokenNot) {
		value, err := p.parseUnary()
		return !truthy(value), err
	}
	return p.parsePrimary()
}

func (p *parser) parsePrimary() (interface{}, *model.EvaluationError) {
	current := p.advance()
	switch current.kind {
	case tokenString:
		return current.text, nil
	case tokenNumber:
		value, err := strconv.ParseFloat(current.text, 64)
		if err != nil {
			return nil, evaluationError("expression.invalid_number", err.Error(), current.pos)
		}
		return value, nil
	case tokenIdentifier:
		if p.match(tokenLParen) {
			return p.parseCall(current)
		}
		if p.skip {
			return false, nil
		}
		switch strings.ToLower(current.text) {
		case "true":
			return true, nil
		case "false", "null":
			return false, nil
		default:
			value, ok := resolve(p.ctx.Values, current.text)
			if !ok {
				if p.allowUnknown {
					return current.text, nil
				}
				if p.provider == model.ProviderGitHub && contextRootExists(p.ctx.Values, current.text) {
					return "", nil
				}
				return nil, evaluationError("expression.unknown_context", "unknown context value "+current.text, current.pos)
			}
			return value, nil
		}
	case tokenLParen:
		value, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		if !p.match(tokenRParen) {
			return nil, evaluationError("expression.missing_parenthesis", "expected closing parenthesis", p.peek().pos)
		}
		return value, nil
	default:
		return nil, evaluationError("expression.expected_value", "expected a value", current.pos)
	}
}

func (p *parser) parseCall(name token) (interface{}, *model.EvaluationError) {
	args := []interface{}{}
	if !p.match(tokenRParen) {
		for {
			value, err := p.parseExpression()
			if err != nil {
				return nil, err
			}
			args = append(args, value)
			if p.match(tokenRParen) {
				break
			}
			if !p.match(tokenComma) {
				return nil, evaluationError("expression.expected_comma", "expected comma", p.peek().pos)
			}
		}
	}
	if p.skip {
		return false, nil
	}
	return p.call(name, args)
}

func (p *parser) call(name token, args []interface{}) (interface{}, *model.EvaluationError) {
	lower := strings.ToLower(name.text)
	switch lower {
	case "success", "succeeded":
		return p.ctx.Status == "" || p.ctx.Status == model.RunSucceeded || p.ctx.Status == model.RunRunning, nil
	case "failure", "failed":
		return p.ctx.Status == model.RunFailed, nil
	case "always":
		return true, nil
	case "cancelled", "canceled":
		return p.ctx.Status == model.RunCancelled, nil
	case "contains", "startswith", "endswith":
		if len(args) != 2 {
			return nil, evaluationError("expression.argument_count", name.text+" expects two arguments", name.pos)
		}
		left, right := stringify(args[0]), stringify(args[1])
		switch lower {
		case "contains":
			return strings.Contains(left, right), nil
		case "startswith":
			return strings.HasPrefix(left, right), nil
		default:
			return strings.HasSuffix(left, right), nil
		}
	case "matches":
		if len(args) != 2 {
			return nil, evaluationError("expression.argument_count", "matches expects two arguments", name.pos)
		}
		matched, err := path.Match(stringify(args[1]), stringify(args[0]))
		if err != nil {
			return nil, evaluationError("expression.invalid_pattern", err.Error(), name.pos)
		}
		return matched, nil
	case "regex":
		if len(args) != 2 {
			return nil, evaluationError("expression.argument_count", "regex expects two arguments", name.pos)
		}
		matched, err := regexp.MatchString(stringify(args[1]), stringify(args[0]))
		if err != nil {
			return nil, evaluationError("expression.invalid_pattern", err.Error(), name.pos)
		}
		return matched, nil
	case "get":
		if len(args) != 2 {
			return nil, evaluationError("expression.argument_count", "get expects two arguments", name.pos)
		}
		key := stringify(args[1])
		switch values := args[0].(type) {
		case map[string]interface{}:
			value, ok := values[key]
			if !ok {
				if p.provider == model.ProviderGitHub {
					return "", nil
				}
				return nil, evaluationError("expression.unknown_context", "unknown context value "+key, name.pos)
			}
			return value, nil
		case map[string]string:
			value, ok := values[key]
			if !ok {
				if p.provider == model.ProviderGitHub {
					return "", nil
				}
				return nil, evaluationError("expression.unknown_context", "unknown context value "+key, name.pos)
			}
			return value, nil
		default:
			if p.allowUnknown {
				return key, nil
			}
			return nil, evaluationError("expression.invalid_argument", "get expects a map", name.pos)
		}
	case "changed":
		if len(args) != 1 {
			return nil, evaluationError("expression.argument_count", "changed expects one argument", name.pos)
		}
		value, ok := resolve(p.ctx.Values, "changed")
		if !ok {
			return false, nil
		}
		pattern := stringify(args[0])
		switch paths := value.(type) {
		case []string:
			for _, candidate := range paths {
				if matched, _ := path.Match(pattern, candidate); matched {
					return true, nil
				}
			}
		case []interface{}:
			for _, candidate := range paths {
				if matched, _ := path.Match(pattern, stringify(candidate)); matched {
					return true, nil
				}
			}
		}
		return false, nil
	case "eq", "ne":
		if len(args) != 2 {
			return nil, evaluationError("expression.argument_count", name.text+" expects two arguments", name.pos)
		}
		value := equals(args[0], args[1])
		if lower == "ne" {
			value = !value
		}
		return value, nil
	case "and", "or":
		if len(args) < 2 {
			return nil, evaluationError("expression.argument_count", name.text+" expects at least two arguments", name.pos)
		}
		value := lower == "and"
		for _, arg := range args {
			if lower == "and" {
				value = value && truthy(arg)
			} else {
				value = value || truthy(arg)
			}
		}
		return value, nil
	case "not":
		if len(args) != 1 {
			return nil, evaluationError("expression.argument_count", "not expects one argument", name.pos)
		}
		return !truthy(args[0]), nil
	default:
		return nil, evaluationError("expression.unknown_function", "unknown function "+name.text, name.pos)
	}
}

func resolve(values map[string]interface{}, path string) (interface{}, bool) {
	parts := strings.Split(path, ".")
	var current interface{} = values
	for _, part := range parts {
		switch typed := current.(type) {
		case map[string]interface{}:
			current, _ = typed[part]
			if current == nil {
				return nil, false
			}
		case map[string]string:
			value, ok := typed[part]
			if !ok {
				return nil, false
			}
			current = value
		default:
			return nil, false
		}
	}
	return current, true
}

func contextRootExists(values map[string]interface{}, path string) bool {
	root, _, _ := strings.Cut(path, ".")
	_, ok := values[root]
	return ok
}

func truthy(value interface{}) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case bool:
		return typed
	case string:
		return typed != "" && !strings.EqualFold(typed, "false") && typed != "0"
	case float64:
		return typed != 0
	case int:
		return typed != 0
	default:
		return true
	}
}

func equals(left, right interface{}) bool {
	return strings.EqualFold(stringify(left), stringify(right))
}

func stringify(value interface{}) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case bool:
		return strconv.FormatBool(typed)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case fmt.Stringer:
		return typed.String()
	default:
		return fmt.Sprintf("%v", typed)
	}
}

func (p *parser) peek() token {
	return p.tokens[p.index]
}

func (p *parser) advance() token {
	current := p.peek()
	if current.kind != tokenEOF {
		p.index++
	}
	return current
}

func (p *parser) match(kind tokenKind) bool {
	if p.peek().kind != kind {
		return false
	}
	p.advance()
	return true
}

func evaluationError(code, message string, position int) *model.EvaluationError {
	return &model.EvaluationError{Code: code, Message: message, Position: position}
}
