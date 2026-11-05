"""Build with an isolated copy of pinned MyGo input and accessibility fixes."""

import hashlib
import json
import pathlib
import shutil
import subprocess
import sys
import tempfile


def main():
    toolchain, command, *args = sys.argv[1:]
    module = json.loads(subprocess.check_output(
        [toolchain, "list", "-m", "-json", "github.com/egoist/mygo"], text=True
    ))
    if module["Version"] != "v0.6.3-0.20261022014156-976f17c9391f":
        raise SystemExit("Review the MyGo input paint patch before changing MyGo versions.")
    if "Replace" in module:
        raise SystemExit("Review the MyGo input paint patch before using a module replacement.")
    if "Dir" not in module:
        module = json.loads(subprocess.check_output(
            [toolchain, "mod", "download", "-json", module["Path"] + "@" + module["Version"]],
            text=True,
        ))
    root = pathlib.Path(__file__).resolve().parent.parent
    source = pathlib.Path(module["Dir"])
    edits = {
        "ui/editor_layout.go": [
            (
                "p.textLayout(pl, e.x+e.contentX(), oy, t.TextMuted, ts, nil)",
                """placeholderColor := t.TextMuted.Alpha(0.4)
        if t.Dark {
            placeholderColor = t.TextMuted.Alpha(0.65)
        }
        p.textLayout(pl, e.x+e.contentX(), oy, placeholderColor, ts, nil)""",
            ),
        ],
        "ui/textarea.go": [],
    }
    for changes in edits.values():
        changes.extend([
            ("op.Rect.W = max(round(p.scale), 1)",
             "op.Rect.W = max(round(2*p.scale), 1)"),
            ("Color: t.Accent.scene(), Wide: p.wide(t.Accent, Color{}, Color{})",
             "Color: ts.color.scene(), Wide: p.wide(ts.color, Color{}, Color{})"),
        ])
    # Let multiline inputs handle Cmd+Enter through the editor's normal insertion,
    # preserving the selection, caret, undo history and native IME routing.
    edits["ui/editor_navigation.go"] = [(
        "case KeyEnter:\n\t\treturn m == 0",
        "case KeyEnter:\n\t\treturn m == 0 || (ed.multiline && k.mods == Cmd)",
    )]
    # Repositioning inside an already focused editor starts a visible caret
    # phase too; focus changes alone do not cover repeated pointer clicks.
    edits["ui/input.go"] = [(
        "\tif target.editor != nil {\n\t\ttarget.editor.pressMods = mods",
        "\tif target.editor != nil {\n"
        "\t\tif button == 0 && target.editor.laidOut() {\n"
        "\t\t\trt.blinkStart = rt.now()\n"
        "\t\t}\n"
        "\t\ttarget.editor.pressMods = mods",
    )]
    # AX setters cache their argument in AppKit even when the next projected
    # password value is still empty. Redact that cache, retaining the original
    # argument only for the editor action.
    edits["internal/darwin/access.go"] = [
        (
            'method("setAccessibilityValue:", func(self id, cmd objc.SEL, value id) {\n'
            '\t\t\tsendSuper(self, "MyGoAccessibilityElement", cmd, uintptr(value))',
            'method("setAccessibilityValue:", func(self id, cmd objc.SEL, value id) {\n'
            '\t\t\tcached := value\n'
            '\t\t\tif el := b().accessElementOf(self); el != nil && el.node.States&platform.AccessPassword != 0 {\n'
            '\t\t\t\tcached = nsString("")\n'
            '\t\t\t}\n'
            '\t\t\tsendSuper(self, "MyGoAccessibilityElement", cmd, uintptr(cached))',
        ),
        (
            'method("setAccessibilitySelectedText:", func(self id, cmd objc.SEL, text id) {\n'
            '\t\t\tsendSuper(self, "MyGoAccessibilityElement", cmd, uintptr(text))',
            'method("setAccessibilitySelectedText:", func(self id, cmd objc.SEL, text id) {\n'
            '\t\t\tcached := text\n'
            '\t\t\tif el := b().accessElementOf(self); el != nil && el.node.States&platform.AccessPassword != 0 {\n'
            '\t\t\t\tcached = nsString("")\n'
            '\t\t\t}\n'
            '\t\t\tsendSuper(self, "MyGoAccessibilityElement", cmd, uintptr(cached))',
        ),
    ]
    edits["ui/list.go"] = [(
        "func (s *ListState) sync(e *node, n int) {\n\tst, rt := e.st, e.c.rt",
        "func (s *ListState) sync(e *node, n int) {\n\tst, rt := e.st, e.c.rt\n"
        "\tif !s.FollowEnd {\n\t\ts.following = false\n\t}",
    )]
    # Keep bullets compact while ordered lists retain room for tabular digits.
    edits["ui/markdown/view.go"] = [(
        "width := th.em(th.ListIndent)\n\tif b.Ordered {",
        "width := th.em(th.ListIndent)\n\tif !b.Ordered {\n"
        "\t\twidth = min(width, th.em(1.4))\n\t}\n\tif b.Ordered {",
    )]
    edits["ui/markdown/view.go"].append((
        'ui.Text(c, bullets[min(depth, 2)]).TextColor(th.Muted).Width(width - th.em(0.5)).TextAlign(ui.Center)',
        '''ui.Text(c, bullets[min(depth, 2)]).TextColor(th.Muted.Alpha(0)).Width(width - th.em(0.5)).TextAlign(ui.Center).Draw(func(p *ui.Painter, r ui.Rect) {
                size := th.em(0.28)
                marker := ui.Rect{X: r.X + (r.W-size)/2, Y: r.Y + (r.H-size)/2 + th.em(0.08), W: size, H: size}
                switch depth {
                case 0:
                    p.Fill(marker, th.Muted, size/2)
                case 1:
                    p.Stroke(marker, th.Muted, size/2, th.em(0.055))
                default:
                    p.Fill(marker, th.Muted, 0)
                }
            })''',
    ))
    edits["ui/markdown/view.go"].append((
        "case st.o.OnLink != nil:",
        'case st.o.OnLink != nil && !strings.HasPrefix(dest, "#"):',
    ))
    edits["ui/split.go"] = [(
        "line := t.Border\n\t\t\t\tif held || handle.Hovered() {\n\t\t\t\t\tline = t.Accent\n\t\t\t\t}\n\t\t\t\tp.Fill(r, line, 0)",
        "p.Fill(r, t.Border, 0)",
    )]
    # Go forbids overlays inside GOMODCACHE, so patch an isolated project cache.
    revision = hashlib.sha256(pathlib.Path(__file__).read_bytes()).hexdigest()[:16]
    cache = root / ".cache" / "mygo-input-style"
    cache.mkdir(parents=True, exist_ok=True)
    patched_module = cache / revision
    if not patched_module.exists():
        with tempfile.TemporaryDirectory(prefix="prepare-", dir=cache) as tmp:
            prepared = pathlib.Path(tmp) / "mygo"
            shutil.copytree(source, prepared)
            prepared.chmod(0o755)
            for name, changes in edits.items():
                original = source / name
                content = original.read_text()
                for before, after in changes:
                    if content.count(before) != 1:
                        raise SystemExit(f"MyGo input paint changed: review {name}: {before}")
                    content = content.replace(before, after, 1)
                patched = prepared / name
                patched.chmod(0o644)
                patched.write_text(content)
            try:
                prepared.rename(patched_module)
            except OSError:
                if not patched_module.is_dir():
                    raise
    # A temporary modfile leaves the checked-in module and sums unchanged.
    with tempfile.TemporaryDirectory(prefix="fuyu-mygo-") as tmp:
        modfile = pathlib.Path(tmp) / "go.mod"
        modfile.write_text(
            (root / "go.mod").read_text()
            + "\nreplace github.com/egoist/mygo => " + json.dumps(str(patched_module)) + "\n"
        )
        shutil.copyfile(root / "go.sum", pathlib.Path(tmp) / "go.sum")
        return subprocess.call([toolchain, command, f"-modfile={modfile}", *args])


if __name__ == "__main__":
    sys.exit(main())
