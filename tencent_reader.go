package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var (
	tencentChapter = regexp.MustCompile(`^/(?:ComicView|chapter)/index/id/[0-9]+/cid/[0-9]+/?$`)
	tencentData    = regexp.MustCompile(`\bvar\s+DATA\s*=\s*'([A-Za-z0-9+/=]+)'\s*[,;]`)
	tencentNonce   = regexp.MustCompile(`window(?:\.nonce|\s*\[\s*["'](?:n|no|non|nonc)["']\s*\+\s*["'](?:once|nce|ce|e)["']\s*\])\s*=\s*([^;\r\n]{1,900});`)
	nonceLiteral   = regexp.MustCompile(`^(?:"([A-Za-z0-9]*)"|'([A-Za-z0-9]*)')`)
	nonceNumber    = regexp.MustCompile(`^\(\s*\+\s*eval\(\s*("(?:\\.|[^"\\])*")\s*\)\s*\)\.toString\(\s*\)`)
	nonceInserts   = regexp.MustCompile(`([0-9]+)([A-Za-z]+)`)
	nonceSubstring = regexp.MustCompile(`^'([0-9]*)'\.(?:substring|slice)\(([0-9]+)\)$`)
)

func parseTencentPages(ctx context.Context, client *http.Client, chapter *url.URL) ([]item, error) {
	if chapter.Scheme != "https" || !tencentReaderHost(chapter) || !tencentChapter.MatchString(chapter.Path) {
		return nil, errors.New("腾讯动漫章节地址格式无效")
	}
	body, final, err := fetch(ctx, client, chapter, 8<<20)
	if err != nil {
		return nil, err
	}
	if !tencentReaderHost(final) || !tencentChapter.MatchString(final.Path) {
		return nil, errors.New("腾讯动漫没有提供公开阅读页")
	}
	return decodeTencentPages(string(body), final)
}

func tencentReaderHost(target *url.URL) bool {
	return target.Hostname() == "ac.qq.com" || target.Hostname() == "m.ac.qq.com"
}

func decodeTencentPages(body string, chapter *url.URL) ([]item, error) {
	dataAt := tencentData.FindStringSubmatchIndex(body)
	if dataAt == nil {
		return nil, errors.New("腾讯动漫阅读页缺少图片数据")
	}
	assignments := tencentNonce.FindAllStringSubmatch(body[:dataAt[0]], -1)
	if len(assignments) == 0 {
		return nil, errors.New("腾讯动漫阅读页缺少排版参数")
	}
	nonce, ok := parseTencentNonce(assignments[len(assignments)-1][1])
	if !ok {
		return nil, errors.New("腾讯动漫阅读页的排版参数格式已变更")
	}
	packed := body[dataAt[2]:dataAt[3]]
	inserts := nonceInserts.FindAllStringSubmatch(nonce, -1)
	if len(inserts) == 0 {
		return nil, errors.New("腾讯动漫阅读页的排版参数无效")
	}
	for i := len(inserts) - 1; i >= 0; i-- {
		value, err := strconv.ParseUint(inserts[i][1], 10, 64)
		start := int(value & 255)
		end := start + len(inserts[i][2])
		if err != nil || end > len(packed) {
			return nil, errors.New("腾讯动漫图片数据格式无效")
		}
		packed = packed[:start] + packed[end:]
	}
	raw, err := base64.StdEncoding.DecodeString(packed)
	if err != nil {
		raw, err = base64.RawStdEncoding.DecodeString(packed)
		if err != nil {
			return nil, errors.New("腾讯动漫图片数据无法解析")
		}
	}
	var data struct {
		Chapter struct {
			CanRead bool `json:"canRead"`
		} `json:"chapter"`
		Picture []struct {
			URL string `json:"url"`
		} `json:"picture"`
	}
	if json.Unmarshal(raw, &data) != nil {
		return nil, errors.New("腾讯动漫图片列表格式无效")
	}
	if !data.Chapter.CanRead {
		return nil, errors.New("腾讯动漫未开放本章的公开读图，请在原站打开")
	}
	pages := make([]item, 0, len(data.Picture))
	for _, picture := range data.Picture {
		if strings.TrimSpace(picture.URL) == "" {
			continue
		}
		image, err := resolveURL(chapter, picture.URL)
		if err != nil {
			return nil, err
		}
		pages = append(pages, item{URL: image.String()})
	}
	return pages, nil
}

// The page builds its nonce out of literals and small numeric expressions.
// Accept that data format without executing the page's JavaScript.
func parseTencentNonce(expression string) (string, bool) {
	var value strings.Builder
	for expression = strings.TrimSpace(expression); ; {
		if literal := nonceLiteral.FindStringSubmatch(expression); len(literal) != 0 {
			value.WriteString(literal[1])
			value.WriteString(literal[2])
			expression = expression[len(literal[0]):]
		} else if part := nonceNumber.FindStringSubmatch(expression); len(part) != 0 {
			input, err := strconv.Unquote(part[1])
			if err != nil {
				return "", false
			}
			number, ok := tencentNumber(input)
			if !ok || math.IsNaN(number) || math.IsInf(number, 0) {
				return "", false
			}
			if number == 0 {
				number = 0 // Match JavaScript's string form of zero.
			}
			value.WriteString(strconv.FormatFloat(number, 'f', -1, 64))
			expression = expression[len(part[0]):]
		} else {
			return "", false
		}
		expression = strings.TrimSpace(expression)
		if expression == "" {
			return value.String(), true
		}
		if expression[0] != '+' {
			return "", false
		}
		expression = strings.TrimSpace(expression[1:])
	}
}

func tencentNumber(expression string) (float64, bool) {
	expression = strings.TrimSpace(expression)
	if cond, branches, ok := strings.Cut(expression, "?"); ok {
		first, second, found := strings.Cut(branches, ":")
		check, valid := tencentNumber(cond)
		if !found || !valid {
			return 0, false
		}
		if check != 0 {
			return tencentNumber(first)
		}
		return tencentNumber(second)
	}
	if parts := nonceSubstring.FindStringSubmatch(expression); len(parts) != 0 {
		start, err := strconv.Atoi(parts[2])
		if err != nil {
			return 0, false
		}
		if start >= len(parts[1]) {
			return 0, true
		}
		number, err := strconv.ParseFloat(parts[1][start:], 64)
		return number, err == nil
	}
	// The public reader also uses ordinary browser objects as truthy values.
	expression = strings.NewReplacer(
		"document.getElementsByTagName('html')", "1",
		`document.getElementsByTagName("html")`, "1",
		"document.children", "1",
		"window.Array", "1",
		"===", "==",
		"!==", "!=",
	).Replace(expression)
	expr, err := parser.ParseExpr(expression)
	if err != nil {
		return 0, false
	}
	return tencentArithmetic(expr)
}

func tencentArithmetic(expr ast.Expr) (float64, bool) {
	switch node := expr.(type) {
	case *ast.ParenExpr:
		return tencentArithmetic(node.X)
	case *ast.BasicLit:
		value, err := strconv.ParseFloat(node.Value, 64)
		return value, err == nil
	case *ast.Ident:
		switch node.Name {
		case "true":
			return 1, true
		case "false":
			return 0, true
		}
	case *ast.UnaryExpr:
		value, ok := tencentArithmetic(node.X)
		if !ok {
			return 0, false
		}
		switch node.Op {
		case token.ADD:
			return value, true
		case token.SUB:
			return -value, true
		case token.NOT:
			return tencentTruth(value == 0), true
		case token.TILDE:
			return float64(^int32(value)), true
		}
	case *ast.BinaryExpr:
		left, leftOK := tencentArithmetic(node.X)
		right, rightOK := tencentArithmetic(node.Y)
		if !leftOK || !rightOK {
			return 0, false
		}
		switch node.Op {
		case token.ADD:
			return left + right, true
		case token.SUB:
			return left - right, true
		case token.MUL:
			return left * right, true
		case token.QUO:
			return left / right, true
		case token.REM:
			return math.Mod(left, right), true
		case token.AND:
			return float64(int32(left) & int32(right)), true
		case token.OR:
			return float64(int32(left) | int32(right)), true
		case token.XOR:
			return float64(int32(left) ^ int32(right)), true
		case token.SHL:
			return float64(int32(left) << (uint32(right) & 31)), true
		case token.SHR:
			return float64(int32(left) >> (uint32(right) & 31)), true
		case token.LAND:
			if left == 0 {
				return left, true
			}
			return right, true
		case token.LOR:
			if left != 0 {
				return left, true
			}
			return right, true
		case token.EQL:
			return tencentTruth(left == right), true
		case token.NEQ:
			return tencentTruth(left != right), true
		case token.LSS:
			return tencentTruth(left < right), true
		case token.LEQ:
			return tencentTruth(left <= right), true
		case token.GTR:
			return tencentTruth(left > right), true
		case token.GEQ:
			return tencentTruth(left >= right), true
		}
	case *ast.CallExpr:
		return tencentNumericCall(node)
	}
	return 0, false
}

func tencentNumericCall(call *ast.CallExpr) (float64, bool) {
	if len(call.Args) == 0 || len(call.Args) > 2 {
		return 0, false
	}
	value, ok := tencentArithmetic(call.Args[0])
	if !ok {
		return 0, false
	}
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		if fn.Name == "parseInt" && len(call.Args) == 1 {
			return math.Trunc(value), true
		}
	case *ast.SelectorExpr:
		object, ok := fn.X.(*ast.Ident)
		if !ok || object.Name != "Math" {
			return 0, false
		}
		if fn.Sel.Name == "pow" && len(call.Args) == 2 {
			exponent, ok := tencentArithmetic(call.Args[1])
			return math.Pow(value, exponent), ok
		}
		if len(call.Args) == 1 {
			switch fn.Sel.Name {
			case "floor":
				return math.Floor(value), true
			case "ceil":
				return math.Ceil(value), true
			case "round":
				return math.Floor(value + 0.5), true
			case "abs":
				return math.Abs(value), true
			}
		}
	}
	return 0, false
}

func tencentTruth(value bool) float64 {
	if value {
		return 1
	}
	return 0
}
