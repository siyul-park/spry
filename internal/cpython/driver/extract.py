"""Extract driver: derive corpus requests from an extracted CPython source tree.

usage: extract.py <cache-root> snippets|syntax|tokenize|encoding|files

stdout JSONL requests {"id", "origin", "mode", "src" | "file", "sha256"}
(sha256 only with "file"; mode is exec|eval|single|tokenize). Output is sorted
by id and deterministic. Notes about skipped sources go to stderr.
"""

import ast
import doctest
import hashlib
import json
import os
import sys
import textwrap

TEST = "Lib/test"
SNIPPETS = "Lib/test/test_ast/snippets.py"
TOKENIZE_TEST = "Lib/test/test_tokenize.py"
TOKENIZE_DATA = "Lib/test/tokenizedata"
ENCODED_MODULES = "Lib/test/encoded_modules"

# Test modules whose docstring or __test__ doctests exercise syntax.
DOCTEST_MODULES = [
    "test_syntax",
    "test_generators",
    "test_extcall",
    "test_pep646_syntax",
    "test_unpack_ex",
    "test_genexps",
    "test_unpack",
    "test_setcomps",
    "test_listcomps",
    "test_dictcomps",
    "test_metaclass",
    "test_descrtut",
]
# Test modules scanned for calls that take a source string.
CALL_MODULES = [
    "test_syntax",
    "test_exceptions",
    "test_type_params",
    "test_patma",
    "test_positional_only_arg",
    "test_keywordonlyarg",
    "test_named_expressions",
    "test_except_star",
    "test_type_aliases",
    "test_eof",
    "test_string_literals",
    "test_unicode_identifiers",
    "test_fstring",
    "test_tstring",
    "test_grammar",
    "test_compile",
    "test_scope",
    "test_with",
    "test_ast/test_ast",
    "test_unparse",
    "test_codeop",
    "test_future_stmt/test_future",
    "test_coroutines",
    "test_source_encoding",
    "test_utf8source",
    "test_flufl",
    "test_decorators",
    "test_dictcomps",
    "test_listcomps",
    "test_setcomps",
    "test_genexps",
    "test_global",
    "test_asyncgen",
    "test_generators",
    "test_dis",
    "test_peepholer",
    "test_class",
    "test_raise",
    "test_unpack_ex",
]
# Builtin-style sinks: name -> default mode. compile and ast.parse also read a mode argument.
BUILTIN_SINKS = {"compile": "exec", "exec": "exec", "eval": "eval", "parse": "exec"}
# Helper sinks taking the source as their first string argument: name -> mode.
HELPER_SINKS = {
    "check_syntax_error": "exec",
    "_check_error": "exec",
    "assertRaisesSyntaxError": "exec",
    "assert_syntax_error": "exec",
    "assertSyntaxError": "exec",
    "run_code": "exec",
    "check_invalid": "exec",
    "check_ast_roundtrip": "exec",
    "check_src_roundtrip": "exec",
    "_exec_future": "exec",
    "compile_single": "single",
    "assertInvalidSingle": "single",
    "ai": "single",
    "av": "single",
}
# Sinks whose first string argument is a list or tuple of sources, by file stem.
LIST_SINKS = {"assertAllRaise": 2}
FILE_SCOPED_SINKS = {"check": "test_exceptions"}
FLAG_KEYWORDS = {
    "flags",
    "dont_inherit",
    "optimize",
    "feature_version",
    "_feature_version",
}
MODES = {"exec", "eval", "single"}


def sha256(data):
    return hashlib.sha256(data).hexdigest()


def sorted_files(cache, root, suffixes):
    base = os.path.join(cache, root)
    found = []
    for dirpath, dirs, files in os.walk(base):
        dirs.sort()
        for name in sorted(files):
            if name.endswith(suffixes):
                found.append(
                    os.path.relpath(os.path.join(dirpath, name), cache).replace(
                        os.sep, "/"
                    )
                )
    return sorted(found)


def read(cache, rel):
    with open(os.path.join(cache, rel), "rb") as f:
        return f.read()


def file_case(cache, prefix, rel, mode):
    return {
        "id": "%s/%s" % (prefix, rel),
        "origin": rel,
        "mode": mode,
        "file": rel,
        "sha256": sha256(read(cache, rel)),
    }


def const_str(node):
    """Source text of a literal str expression, or None."""
    if isinstance(node, ast.Constant) and isinstance(node.value, str):
        return node.value
    if isinstance(node, ast.BinOp) and isinstance(node.op, ast.Add):
        left, right = const_str(node.left), const_str(node.right)
        return None if left is None or right is None else left + right
    if isinstance(node, ast.Call) and len(node.args) == 1 and not node.keywords:
        name = call_name(node)
        inner = const_str(node.args[0])
        if name == "dedent" and inner is not None:
            return textwrap.dedent(inner)
    return None


def call_name(call):
    f = call.func
    return (
        f.attr
        if isinstance(f, ast.Attribute)
        else f.id
        if isinstance(f, ast.Name)
        else None
    )


def encodable(src):
    try:
        src.encode("utf-8")
        return True
    except UnicodeEncodeError:
        return False


def mode_arg(call, index):
    """Mode of compile-like call: (mode, ok). ok is False when it is not a plain literal."""
    node = call.args[index] if len(call.args) > index else None
    for kw in call.keywords:
        if kw.arg == "mode":
            node = kw.value
    if node is None:
        return None, True
    if isinstance(node, ast.Constant) and node.value in MODES:
        return node.value, True
    return None, False


def sink_sources(call, stem):
    """Yield (src, mode) pairs for a call that feeds source text to the compiler."""
    name = call_name(call)
    if any(kw.arg in FLAG_KEYWORDS for kw in call.keywords):
        return
    if name in BUILTIN_SINKS:
        if isinstance(call.func, ast.Attribute) and not (
            isinstance(call.func.value, ast.Name)
            and call.func.value.id == "ast"
            and name == "parse"
        ):
            return
        if name in ("compile", "parse"):
            if len(call.args) > 3:
                return
            mode, ok = mode_arg(call, 2)
            if not ok:
                return
            mode = mode or "exec"
        else:
            mode = BUILTIN_SINKS[name]
        src = const_str(call.args[0]) if call.args else None
        if src is not None:
            yield src, mode
        return
    mode = HELPER_SINKS.get(name) or (
        name in FILE_SCOPED_SINKS and FILE_SCOPED_SINKS[name] == stem and "exec"
    )
    if mode:
        args = call.args
        if args and isinstance(args[0], ast.Name) and args[0].id in ("self", "test"):
            args = args[1:]
        src = const_str(args[0]) if args else None
        if src is not None:
            yield src, mode
    elif name in LIST_SINKS and len(call.args) > LIST_SINKS[name]:
        seq = call.args[LIST_SINKS[name]]
        if isinstance(seq, (ast.List, ast.Tuple)):
            for elt in seq.elts:
                src = const_str(elt)
                if src is not None:
                    yield src, "exec"


def call_cases(cache, rel, stem):
    tree = ast.parse(read(cache, rel))
    calls = sorted(
        (n for n in ast.walk(tree) if isinstance(n, ast.Call)),
        key=lambda n: (n.lineno, n.col_offset),
    )
    for call in calls:
        for src, mode in sink_sources(call, stem):
            yield src, mode, "%s:%d" % (rel, call.lineno)


def doctest_cases(cache, rel):
    tree = ast.parse(read(cache, rel))
    consts = sorted(
        (
            n
            for n in ast.walk(tree)
            if isinstance(n, ast.Constant)
            and isinstance(n.value, str)
            and ">>> " in n.value
        ),
        key=lambda n: (n.lineno, n.col_offset),
    )
    parser = doctest.DocTestParser()
    for node in consts:
        try:
            examples = parser.get_examples(node.value)
        except ValueError:
            continue
        for ex in examples:
            yield ex.source, "single", "%s:%d" % (rel, node.lineno + ex.lineno)


def syntax(cache):
    """Doctest examples and call sinks, deduplicated by (source, mode)."""
    seen, out, counters, skipped = set(), [], {}, 0
    modules = [("doctest", m, doctest_cases) for m in DOCTEST_MODULES]
    modules += [("call", m, call_cases) for m in CALL_MODULES]
    for kind, module, gen in sorted(modules, key=lambda m: (m[1], m[0])):
        rel = "%s/%s.py" % (TEST, module)
        if not os.path.exists(os.path.join(cache, rel)):
            print("missing %s" % rel, file=sys.stderr)
            continue
        stem = module.split("/")[-1]
        args = (cache, rel, stem) if gen is call_cases else (cache, rel)
        for src, mode, origin in gen(*args):
            if not encodable(src):
                skipped += 1
                continue
            key = (src, mode)
            if key in seen:
                continue
            seen.add(key)
            n = counters[(module, kind)] = counters.get((module, kind), 0) + 1
            out.append(
                {
                    "id": "%s/%s/%04d" % (module.replace("/", "."), kind, n),
                    "origin": origin,
                    "mode": mode,
                    "src": src,
                }
            )
    print("skipped %d sources with lone surrogates" % skipped, file=sys.stderr)
    return out


def snippets(cache):
    tree = ast.parse(read(cache, SNIPPETS))
    out = []
    for stmt in tree.body:
        if isinstance(stmt, ast.Assign) and isinstance(stmt.targets[0], ast.Name):
            name = stmt.targets[0].id
            if name in ("exec_tests", "single_tests", "eval_tests"):
                mode = name.split("_")[0]
                for i, elt in enumerate(stmt.value.elts):
                    out.append(
                        {
                            "id": "snippets/%s/%03d" % (mode, i),
                            "origin": "%s:%d" % (SNIPPETS, elt.lineno),
                            "mode": mode,
                            "src": ast.literal_eval(elt),
                        }
                    )
    return out


def check_tokenize(cache):
    tree = ast.parse(read(cache, TOKENIZE_TEST))
    out, seen = [], set()
    calls = sorted(
        (
            n
            for n in ast.walk(tree)
            if isinstance(n, ast.Call) and call_name(n) == "check_tokenize"
        ),
        key=lambda n: (n.lineno, n.col_offset),
    )
    for call in calls:
        src = const_str(call.args[0]) if call.args else None
        if src is None or src in seen or not encodable(src):
            continue
        seen.add(src)
        out.append(
            {
                "id": "test_tokenize/check/%04d" % (len(out) + 1),
                "origin": "%s:%d" % (TOKENIZE_TEST, call.lineno),
                "mode": "tokenize",
                "src": src,
            }
        )
    return out


def tokenize(cache):
    out = check_tokenize(cache)
    data = sorted_files(cache, TOKENIZE_DATA, (".py", ".txt"))
    lib = sorted_files(cache, "Lib", (".py",))
    for rel in sorted(set(data) | set(lib)):
        out.append(file_case(cache, "file", rel, "tokenize"))
    return out


def encoding(cache):
    rels = sorted_files(cache, TOKENIZE_DATA, (".py", ".txt")) + sorted_files(
        cache, ENCODED_MODULES, (".py",)
    )
    return [file_case(cache, "encoding", rel, "exec") for rel in rels]


def files(cache):
    return [
        file_case(cache, "file", rel, "exec")
        for rel in sorted_files(cache, "Lib", (".py",))
    ]


def main():
    cache, group = sys.argv[1], sys.argv[2]
    cases = {
        "snippets": snippets,
        "syntax": syntax,
        "tokenize": tokenize,
        "encoding": encoding,
        "files": files,
    }[group](cache)
    for case in sorted(cases, key=lambda c: c["id"]):
        print(json.dumps(case, sort_keys=True))


main()
