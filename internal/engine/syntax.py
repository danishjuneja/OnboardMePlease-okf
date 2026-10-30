"""Optional isolated Tree-sitter adapter. Reads source data; never imports it."""
import base64
import ast
import glob
import hashlib
import importlib
import importlib.metadata
import json
import os
import sys
import sysconfig

GRAMMARS = {
    ".py": ("python", "language"),
    ".ts": ("typescript", "language_typescript"),
    ".tsx": ("typescript", "language_tsx"),
}
DECLARATIONS = {
    "function_definition", "class_definition", "function_declaration",
    "generator_function_declaration", "method_definition", "class_declaration",
    "interface_declaration", "type_alias_declaration",
}


def probe():
    from tree_sitter import Language, Parser
    available = {}
    for ext, (name, entry) in GRAMMARS.items():
        try:
            module = importlib.import_module("tree_sitter_" + name)
            language = Language(getattr(module, entry)())
            Parser(language)
            available[ext] = {"version": importlib.metadata.version("tree-sitter-" + name), "abi": language.abi_version}
        except (ImportError, AttributeError, TypeError, ValueError, importlib.metadata.PackageNotFoundError):
            continue
    dependencies = {sys.executable, getattr(sys, "_base_executable", sys.executable),
                    sys.base_prefix, os.path.join(sys.prefix, "pyvenv.cfg")}
    libdir = sysconfig.get_config_var("LIBDIR")
    if libdir:
        dependencies.update(glob.glob(os.path.join(libdir, "libpython*.so*")))
        dependencies.update(glob.glob(os.path.join(libdir, "libpython*.dylib")))
    dependencies.update(glob.glob(os.path.join(sys.base_prefix, "python*.dll")))
    dependencies.update(p for p in sys.path if os.path.isabs(p))
    for name in ("tree-sitter", "tree-sitter-python", "tree-sitter-typescript"):
        try:
            distribution = importlib.metadata.distribution(name)
        except importlib.metadata.PackageNotFoundError:
            continue
        for file in distribution.files or []:
            if str(file).endswith((".py", ".pyd", ".so", ".dll", "METADATA", "RECORD")):
                dependencies.add(os.path.abspath(distribution.locate_file(file)))
    digests = {}
    for dependency in sorted(dependencies):
        if os.path.isfile(dependency):
            h = hashlib.sha256()
            with open(dependency, "rb") as stream:
                for block in iter(lambda: stream.read(1024 * 1024), b""):
                    h.update(block)
            digests[dependency] = h.hexdigest()
    return {"python": sys.version, "tree_sitter": importlib.metadata.version("tree-sitter"),
            "grammars": available, "dependencies": sorted(dependencies), "digests": digests}


def text(node, source):
    return source[node.start_byte:node.end_byte].decode("utf-8")


def request(node, source):
    function = node.child_by_field_name("function")
    args = node.child_by_field_name("arguments")
    if not function or text(function, source) != "fetch" or not args:
        return None
    children = args.named_children
    if len(children) < 2 or children[1].type != "object":
        return None
    url = children[0]
    if url.type == "string":
        route = text(url, source)[1:-1]
    elif url.type == "template_string":
        route = "".join("{}" if child.type == "template_substitution" else text(child, source)
                        for child in url.named_children)
    else:
        return None
    method = None
    for pair in children[1].named_children:
        if pair.type != "pair":
            continue
        key, value = pair.child_by_field_name("key"), pair.child_by_field_name("value")
        if key and value and text(key, source).strip("'\"") == "method" and value.type == "string":
            method = text(value, source)[1:-1]
    if method not in ("GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"):
        return None
    return {"line": node.start_point.row + 1, "method": method, "route": route}


def analyze(path, source):
    from tree_sitter import Language, Parser
    ext = "." + path.rsplit(".", 1)[-1].lower() if "." in path else ""
    name, entry = GRAMMARS[ext]
    language = Language(getattr(importlib.import_module("tree_sitter_" + name), entry)())
    root = Parser(language).parse(source).root_node
    result = {"syntax": "tree_sitter_" + name + ("_partial" if root.has_error else ""),
              "declarations": [], "calls": [], "requests": [], "events": []}
    stack = [root]
    while stack:
        node = stack.pop()
        stack.extend(reversed(node.named_children))
        if node.type in DECLARATIONS:
            name_node = node.child_by_field_name("name")
            if name_node:
                start = node.parent if node.parent and node.parent.type in ("decorated_definition", "export_statement") else node
                result["declarations"].append({"line": start.start_point.row + 1, "name": text(name_node, source)})
        if node.type == "variable_declarator":
            name_node, value = node.child_by_field_name("name"), node.child_by_field_name("value")
            if name_node and name_node.type == "identifier" and value and value.type in ("arrow_function", "function_expression"):
                result["declarations"].append({"line": node.start_point.row + 1, "name": text(name_node, source)})
        if node.type in ("call", "call_expression"):
            function = node.child_by_field_name("function")
            if function and function.type == "identifier":
                result["calls"].append({"line": node.start_point.row + 1, "name": text(function, source)})
            elif function and function.type in ("attribute", "member_expression"):
                member = function.child_by_field_name("attribute") or function.child_by_field_name("property")
                if member and member.type in ("identifier", "property_identifier"):
                    result["calls"].append({"line": node.start_point.row + 1, "name": text(member, source)})
            if ext in (".ts", ".tsx"):
                found = request(node, source)
                if found:
                    result["requests"].append(found)
        if ext == ".py" and node.type == "comparison_operator":
            children = node.named_children
            operator = source[children[0].end_byte:children[1].start_byte].strip() if len(children) == 2 else b""
            if len(children) == 2 and children[0].type == "identifier" and children[1].type == "string" and operator in (b"==", b"!="):
                try:
                    value = ast.literal_eval(text(children[1], source))
                    if isinstance(value, str) and value:
                        result["events"].append({"line": node.start_point.row + 1, "topic": value})
                except (ValueError, SyntaxError, RecursionError, OverflowError):
                    pass
    return result


if __name__ == "__main__":
    if len(sys.argv) > 1 and sys.argv[1] == "--probe":
        print(json.dumps(probe(), sort_keys=True))
    else:
        # The Go process sends UTF-8 JSON; Windows text-mode stdin may use a
        # legacy code page even under Python isolated mode.
        items = json.load(sys.stdin.buffer)
        for item in items:
            item["analysis"] = analyze(item["path"], base64.b64decode(item.pop("source", "") or "", validate=True))
        print(json.dumps(items))
