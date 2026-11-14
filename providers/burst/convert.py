#!/usr/bin/env python3
"""Developer step (go generate): converts Burst's providers.json into providers.json for Elementum.

Burst definitions hold Python expressions it runs with eval(). They are converted here into plain JSON
specs that selector.go evaluates. A provider with an expression this script does not understand gets
"unsupported": "<field>: <reason>" and is skipped by Elementum.

Usage: convert.py [path to script.elementum.burst]   (default: ../../../script.elementum.burst)
"""
import ast
import json
import os
import sys


class Unsupported(Exception):
    pass


def lit(node):
    try:
        return ast.literal_eval(node)
    except Exception:
        raise Unsupported("not a literal: " + ast.dump(node)[:60])


def select(value):
    """('class', 'x') | ('class', ['a', 'b']) | [(..), (..)] -> [[key, [values]], ...]"""
    if value is None:
        return None
    if isinstance(value, tuple):
        value = [value]
    return [[k, v if isinstance(v, list) else [v]] for k, v in value]


def call_args(node, names):
    """Positional and keyword arguments of a call as a dict, by the parameter names of the ehp method."""
    if len(node.args) > len(names):
        raise Unsupported("too many arguments")
    args = dict(zip(names, (lit(a) for a in node.args)))
    for kw in node.keywords:
        if kw.arg not in names:
            raise Unsupported("unknown argument " + str(kw.arg))
        args[kw.arg] = lit(kw.value)
    return args


def name_of(func):
    """'item', 'find_all', 'item.find_all', 're.search' ... for a call's func, with the receiver node."""
    if isinstance(func, ast.Name):
        return func.id, None
    if isinstance(func, ast.Attribute):
        return func.attr, func.value
    raise Unsupported("call of " + ast.dump(func)[:40])


def is_root(node):
    """The row (item) or the page: the implicit receiver."""
    return node is None or (isinstance(node, ast.Name) and node.id in ("item", "dom"))


def nodes(node):
    """Expression giving elements: a chain of find_once/find_all -> list of steps."""
    if is_root(node):
        return []
    if isinstance(node, ast.Call):
        fn, recv = name_of(node.func)
        if fn == "find_once":
            a = call_args(node, ["tag", "select", "order"])
            return nodes(recv) + [clean({"tag": a.get("tag"), "select": select(a.get("select")), "nth": a.get("order", 1)})]
        if fn == "find_all":
            a = call_args(node, ["tag", "select", "every", "start"])
            return nodes(recv) + [clean({"tag": a.get("tag"), "select": select(a.get("select")), "all": True,
                                         "start": a.get("start", 1), "every": a.get("every", 1)})]
        if fn == "take_with_root":
            return nodes(recv) + [{"parent_of": select(lit(node.args[0]))}]
    if isinstance(node, ast.Subscript) and isinstance(node.value, ast.Call):
        steps = nodes(node.value)
        index = lit(node.slice)
        if steps and isinstance(index, int):
            last = dict(steps[-1])
            if last.pop("all", None):
                last.pop("start", None), last.pop("every", None)
                last["nth"] = index + 1  # nth 0 is the last element, as ehp's list[order - 1]
                return steps[:-1] + [last]
            if "parent_of" in last and index == 0:
                return steps
    raise Unsupported("elements: " + ast.dump(node)[:60])


def clean(d):
    return dict((k, v) for k, v in d.items() if v is not None and not (k in ("start", "every", "nth") and v == 1))


def text(node):
    """Expression giving a string -> spec."""
    if isinstance(node, ast.Constant) and isinstance(node.value, (str, int)):
        return str(node.value)
    if isinstance(node, ast.Name) and node.id == "key":
        return {"op": "key"}
    if isinstance(node, ast.BinOp) and isinstance(node.op, ast.Mod) and isinstance(node.left, ast.Constant):
        return {"op": "format", "format": node.left.value, "arg": text(node.right)}
    if isinstance(node, ast.BinOp) and isinstance(node.op, ast.Add):
        parts = []
        for side in (node.left, node.right):
            t = text(side)
            parts.extend(t["args"] if isinstance(t, dict) and t.get("op") == "concat" else [t])
        return {"op": "concat", "args": parts}
    if isinstance(node, ast.IfExp):
        return {"op": "if", "test": text(node.test), "then": text(node.body), "else": text(node.orelse)}
    if isinstance(node, ast.Subscript):
        # .attr['href'] on elements
        if isinstance(node.value, ast.Attribute) and node.value.attr == "attr":
            return {"in": nodes(node.value.value), "attr": lit(node.slice), "raw": True}
        # x.split(sep, n)[i]
        if isinstance(node.value, ast.Call) and name_of(node.value.func)[0] == "split":
            call = node.value
            a = [lit(x) for x in call.args]
            return {"op": "split", "arg": text(call.func.value), "sep": a[0], "max": a[1] if len(a) > 1 else -1, "index": lit(node.slice)}
        if isinstance(node.slice, ast.Slice):
            s = node.slice
            return clean({"op": "slice", "arg": text(node.value), "from": lit(s.lower) if s.lower else None, "to": lit(s.upper) if s.upper else None})
        raise Unsupported("subscript")
    if isinstance(node, ast.Call):
        # find_once(...)(tag=..., ...): an element called like item
        inside = nodes(node.func) if isinstance(node.func, ast.Call) else None
        fn, recv = ("item", None) if inside is not None else name_of(node.func)
        if fn == "item" and recv is None:
            a = call_args(node, ["tag", "order", "select", "attribute", "divider"])
            spec = clean({"in": inside, "tag": a.get("tag"), "select": select(a.get("select")), "nth": a.get("order", 1)})
            if a.get("attribute", "text") != "text":
                spec["attr"] = a["attribute"]
            if a.get("divider", ("", 1))[0] != "":
                spec["divider"] = list(a["divider"])
            return spec
        if fn == "text" and not node.args:
            return {"in": nodes(recv), "raw": True}
        if fn == "group" and isinstance(recv, ast.Call) and name_of(recv.func)[0] == "search":
            return {"op": "regex", "pattern": lit(recv.args[0]), "arg": text(recv.args[1]), "group": lit(node.args[0])}
        if fn == "sub" and isinstance(recv, ast.Name) and recv.id == "re":
            return {"op": "resub", "pattern": lit(node.args[0]), "repl": lit(node.args[1]), "arg": text(node.args[2])}
        if fn == "unquote":
            return {"op": "unquote", "arg": text(node.args[0])}
        if fn == "replace":
            return {"op": "replace", "arg": text(recv), "old": lit(node.args[0]), "new": text(node.args[1])}
        if fn == "upper":
            return {"op": "upper", "arg": text(recv)}
    raise Unsupported(ast.dump(node)[:80])


def convert(provider):
    out = dict(provider)
    parser = provider.get("parser") or {}
    converted = {}
    for field, expression in parser.items():
        if not expression:
            continue
        try:
            tree = ast.parse(expression, mode="eval").body
            converted[field] = nodes(tree) if field == "row" else text(tree)
        except (Unsupported, SyntaxError, ValueError, IndexError, TypeError) as e:
            out["unsupported"] = "parser.%s: %s" % (field, str(e) or type(e).__name__)
    out["parser"] = converted
    for field in ("post_data", "headers", "login_object", "login_headers"):
        if provider.get(field):
            try:
                out[field] = form(provider[field])
            except Exception as e:
                out["unsupported"] = "%s: %s" % (field, e)
    return out


def form(expression):
    """A dict literal of a request -> {name: text}. The names USERNAME, PASSWORD and CSRF_TOKEN stay as
    those words, which Elementum replaces; definition['key'] becomes {key}."""
    tree = ast.parse(expression.strip(), mode="eval").body
    if not isinstance(tree, ast.Dict):
        raise Unsupported("not a dict")
    result = {}
    for k, v in zip(tree.keys, tree.values):
        if isinstance(v, ast.Name):
            value = v.id
        elif isinstance(v, ast.Subscript) and isinstance(v.value, ast.Name) and v.value.id == "definition":
            value = "{%s}" % lit(v.slice)
        else:
            value = str(lit(v))
        result[str(lit(k))] = value
    return result


def main():
    here = os.path.dirname(os.path.abspath(__file__))
    burst = sys.argv[1] if len(sys.argv) > 1 else os.environ.get("ELEMENTUM_BURST_PATH") or os.path.join(here, "..", "..", "..", "script.elementum.burst")
    with open(os.path.join(burst, "burst", "providers", "providers.json"), encoding="utf-8") as f:
        source = json.load(f)
    result = dict((name, convert(p)) for name, p in sorted(source.items()))
    with open(os.path.join(here, "providers.json"), "w", encoding="utf-8") as f:
        json.dump(result, f, ensure_ascii=False, indent=1, sort_keys=True)
        f.write("\n")
    bad = dict((n, p["unsupported"]) for n, p in result.items() if "unsupported" in p)
    print("%d providers converted, %d unsupported" % (len(result) - len(bad), len(bad)))
    for n, why in sorted(bad.items()):
        print("  %s: %s" % (n, why))


if __name__ == "__main__":
    main()
