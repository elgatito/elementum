package burst

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

// A spec is the JSON form of one Burst parser expression (see convert.py):
//
//	"text"                                             a literal
//	{"tag":"a","nth":2,"attr":"href"}                  the 2nd <a> inside the row, its href (text without attr)
//	{"tag":"td","select":[["class",["a","b"]]]}        the first <td> with class a or b
//	{"in":[steps],"tag":...}                           the same, inside the element the steps lead to
//	{"op":"format","format":"https://x%s","arg":spec}  and the other ops of eval
//
// A row spec is a list of steps: {"tag","select","nth"} picks one element, with "all" (and "start",
// "every") it picks all of them.

var errNoMatch = errors.New("no match")

// elements returns the elements below n named tag whose attributes match sel, children before their
// parent: the order Burst's parser counts in.
func elements(n *html.Node, tag string, sel any) []*html.Node {
	var out []*html.Node
	var walk func(*html.Node)
	walk = func(p *html.Node) {
		for c := p.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			walk(c)
			if c.Data == tag && matches(c, sel) {
				out = append(out, c)
			}
		}
	}
	walk(n)
	return out
}

// matches reports whether any [key, values] pair of sel has a value equal to one of the words of the
// element's attribute. No sel matches everything.
func matches(n *html.Node, sel any) bool {
	pairs, _ := sel.([]any)
	if len(pairs) == 0 {
		return true
	}
	for _, p := range pairs {
		pair, _ := p.([]any)
		if len(pair) != 2 {
			continue
		}
		key, _ := pair[0].(string)
		values, _ := pair[1].([]any)
		for _, word := range strings.Fields(attr(n, key)) {
			for _, v := range values {
				if v == word {
					return true
				}
			}
		}
	}
	return false
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func text(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(p *html.Node) {
		if p.Type == html.TextNode {
			b.WriteString(p.Data)
		}
		for c := p.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

// pick returns element number nth, counted from 1; 0 and below count from the end (0 is the last),
// as Python's list[nth-1] does.
func pick(list []*html.Node, nth int) *html.Node {
	i := nth - 1
	if i < 0 {
		i += len(list)
	}
	if i < 0 || i >= len(list) {
		return nil
	}
	return list[i]
}

func number(v any, def int) int {
	if f, ok := v.(float64); ok {
		return int(f)
	}
	return def
}

// follow walks the steps from n and returns the elements they lead to.
func follow(n *html.Node, steps any) []*html.Node {
	current := []*html.Node{n}
	list, _ := steps.([]any)
	for _, s := range list {
		step, _ := s.(map[string]any)
		var next []*html.Node
		for _, from := range current {
			if sel, ok := step["parent_of"]; ok { // the parent of the first element with these attributes
				if found := elementsAny(from, sel); len(found) > 0 && found[0].Parent != nil {
					next = append(next, found[0].Parent)
				}
				continue
			}
			tag, _ := step["tag"].(string)
			found := elements(from, tag, step["select"])
			if all, _ := step["all"].(bool); !all {
				if e := pick(found, number(step["nth"], 1)); e != nil {
					next = append(next, e)
				}
				continue
			}
			start, every := number(step["start"], 1), max(number(step["every"], 1), 1)
			for i, e := range found {
				if i+1 >= start && (i+1-start)%every == 0 {
					next = append(next, e)
				}
			}
		}
		current = next
	}
	return current
}

// elementsAny is elements for any tag name.
func elementsAny(n *html.Node, sel any) []*html.Node {
	var out []*html.Node
	var walk func(*html.Node)
	walk = func(p *html.Node) {
		for c := p.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode {
				if matches(c, sel) {
					out = append(out, c)
				}
				walk(c)
			}
		}
	}
	walk(n)
	return out
}

// eval computes a spec on a row. key is the page-wide value of the definition's "key" spec.
// An error drops the row, as an exception does in Burst.
func eval(spec any, row *html.Node, key string) (string, error) {
	switch s := spec.(type) {
	case nil:
		return "", nil
	case string:
		return s, nil
	case map[string]any:
		if op, _ := s["op"].(string); op != "" {
			return evalOp(op, s, row, key)
		}
		return evalItem(s, row)
	}
	return "", fmt.Errorf("bad spec %v", spec)
}

func evalItem(s map[string]any, row *html.Node) (string, error) {
	raw, _ := s["raw"].(bool) // .text() and .attr[...] on an element: no trimming, and a missing element is an error
	n := row
	if in, ok := s["in"]; ok {
		found := follow(row, in)
		if len(found) == 0 {
			if raw {
				return "", errNoMatch
			}
			return "", nil
		}
		n = found[0]
	}
	if tag, _ := s["tag"].(string); tag != "" {
		if n = pick(elements(n, tag, s["select"]), number(s["nth"], 1)); n == nil {
			return "", nil
		}
	}
	value := ""
	if name, _ := s["attr"].(string); name != "" {
		value = attr(n, name)
	} else {
		value = text(n)
	}
	if raw {
		return value, nil
	}
	value = strings.TrimSpace(value)
	if div, _ := s["divider"].([]any); len(div) == 2 {
		sep, _ := div[0].(string)
		parts := strings.Split(value, sep)
		if i := number(div[1], 1); i >= 0 && i < len(parts) {
			return strings.TrimSpace(parts[i]), nil
		}
		return "", nil
	}
	return value, nil
}

func evalOp(op string, s map[string]any, row *html.Node, key string) (string, error) {
	if op == "key" {
		return key, nil
	}
	if op == "concat" {
		var b strings.Builder
		parts, _ := s["args"].([]any)
		for _, p := range parts {
			v, err := eval(p, row, key)
			if err != nil {
				return "", err
			}
			b.WriteString(v)
		}
		return b.String(), nil
	}
	if op == "if" {
		if test, err := eval(s["test"], row, key); err == nil && test != "" {
			return eval(s["then"], row, key)
		}
		return eval(s["else"], row, key)
	}
	arg, err := eval(s["arg"], row, key)
	if err != nil {
		return "", err
	}
	str := func(k string) string { v, _ := s[k].(string); return v }
	switch op {
	case "format":
		return strings.Replace(str("format"), "%s", arg, 1), nil
	case "upper":
		return strings.ToUpper(arg), nil
	case "unquote":
		if u, err := url.PathUnescape(arg); err == nil {
			return u, nil
		}
		return arg, nil
	case "replace":
		repl, err := eval(s["new"], row, key)
		return strings.ReplaceAll(arg, str("old"), repl), err
	case "split":
		parts := strings.Split(arg, str("sep"))
		if m := number(s["max"], -1); m >= 0 {
			parts = strings.SplitN(arg, str("sep"), m+1)
		}
		i := number(s["index"], 0)
		if i < 0 {
			i += len(parts)
		}
		if i < 0 || i >= len(parts) {
			return "", errNoMatch
		}
		return parts[i], nil
	case "slice":
		r := []rune(arg)
		from, to := number(s["from"], 0), number(s["to"], len(r))
		if from < 0 {
			from += len(r)
		}
		if to < 0 {
			to += len(r)
		}
		from, to = min(max(from, 0), len(r)), min(max(to, 0), len(r))
		if from > to {
			return "", nil
		}
		return string(r[from:to]), nil
	case "regex", "resub":
		// shortcut: Go's RE2 has no look-arounds or backreferences; no bundled definition uses them.
		re, err := regexp.Compile(str("pattern"))
		if err != nil {
			return "", err
		}
		if op == "resub" {
			return re.ReplaceAllString(arg, str("repl")), nil
		}
		m := re.FindStringSubmatch(arg)
		if g := number(s["group"], 0); m != nil && g < len(m) {
			return m[g], nil
		}
		return "", errNoMatch
	}
	return "", fmt.Errorf("unknown op %q", op)
}
