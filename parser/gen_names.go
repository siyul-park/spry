//go:build ignore

// gen_names generates names.txt.gz, the Unicode character names of CPython's
// unicodedata, for \N{...} escapes. It needs Python 3.14 (SPRY_PYTHON or
// python3.14 on PATH) so that the names match CPython's Unicode version.
//
// Lines are "HEX NAME" for one character, "R LO HI PREFIX" for a range of
// names that end in the code point in hex, and "A HEX NAME" for an alias.
package main

import (
	"bytes"
	"compress/gzip"
	"log"
	"os"
	"os/exec"
)

const script = `
import unicodedata, sys
prefixes = ("CJK UNIFIED IDEOGRAPH-", "CJK COMPATIBILITY IDEOGRAPH-", "TANGUT IDEOGRAPH-",
            "KHITAN SMALL SCRIPT CHARACTER-", "NUSHU CHARACTER-")
out = []
runs = {}
for cp in range(0x110000):
    try:
        name = unicodedata.name(chr(cp))
    except ValueError:
        continue
    if name.startswith("HANGUL SYLLABLE "):
        continue
    pre = next((p for p in prefixes if name.startswith(p)), None)
    if pre and name == pre + "%04X" % cp:
        run = runs.get(pre)
        if run and run[1] == cp - 1:
            run[1] = cp
        else:
            if run: out.append("R %X %X %s" % (run[0], run[1], pre))
            runs[pre] = [cp, cp]
        continue
    out.append("%X %s" % (cp, name))
for pre, run in runs.items():
    out.append("R %X %X %s" % (run[0], run[1], pre))
aliases = """NUL SOH STX ETX EOT ENQ ACK BEL BS HT LF VT FF CR SO SI DLE DC1 DC2 DC3 DC4 NAK SYN ETB CAN EM SUB ESC FS GS RS US SP DEL
BPH NBH NEL SSA ESA HTS HTJ VTS PLD PLU RI SS2 SS3 DCS PU1 PU2 STS CCH MW SPA EPA SOS SGCI SCI CSI ST OSC PM APC NBSP SHY
ZWSP ZWNJ ZWJ LRM RLM LRE RLE PDF LRO RLO WJ BOM ZWNBSP NULL LINE FEED CARRIAGE RETURN CHARACTER TABULATION ESCAPE DELETE
LINE FEED|LINE TABULATION|FORM FEED|CARRIAGE RETURN|CHARACTER TABULATION|NEXT LINE|NO-BREAK SPACE|SOFT HYPHEN|SPACE|BELL|BACKSPACE|ALERT|ESCAPE|DELETE|NULL|END OF LINE|NEW LINE|LF|CR|NEL|ZERO WIDTH NO-BREAK SPACE|BYTE ORDER MARK""".replace("\n", " ").replace("|", " ").split()
extra = ["LINE FEED", "LINE TABULATION", "FORM FEED", "CARRIAGE RETURN", "CHARACTER TABULATION", "NEXT LINE",
         "NO-BREAK SPACE", "SOFT HYPHEN", "END OF LINE", "NEW LINE", "ZERO WIDTH NO-BREAK SPACE", "BYTE ORDER MARK",
         "START OF HEADING", "START OF TEXT", "END OF TEXT", "END OF TRANSMISSION", "ENQUIRY", "ACKNOWLEDGE",
         "ALERT", "BACKSPACE", "NULL", "ESCAPE", "DELETE", "SHIFT OUT", "SHIFT IN", "DATA LINK ESCAPE",
         "NEGATIVE ACKNOWLEDGE", "SYNCHRONOUS IDLE", "END OF TRANSMISSION BLOCK", "CANCEL", "END OF MEDIUM",
         "SUBSTITUTE", "INFORMATION SEPARATOR FOUR", "INFORMATION SEPARATOR THREE", "INFORMATION SEPARATOR TWO",
         "INFORMATION SEPARATOR ONE", "VERTICAL TABULATION", "ZWNBSP", "TAB", "NBSP"]
seen = set()
for a in aliases + extra:
    if a in seen:
        continue
    seen.add(a)
    try:
        c = unicodedata.lookup(a)
    except KeyError:
        continue
    if len(c) != 1:
        continue
    try:
        if unicodedata.name(c) == a:
            continue
    except ValueError:
        pass
    out.append("A %X %s" % (ord(c), a))
sys.stdout.write("\n".join(out) + "\n")
`

func main() {
	py := os.Getenv("SPRY_PYTHON")
	if py == "" {
		py = "python3.14"
	}
	var stdout bytes.Buffer
	cmd := exec.Command(py, "-I", "-c", script)
	cmd.Stdout, cmd.Stderr = &stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		log.Fatal(err)
	}
	var gz bytes.Buffer
	w, _ := gzip.NewWriterLevel(&gz, gzip.BestCompression)
	w.Write(stdout.Bytes())
	if err := w.Close(); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile("names.txt.gz", gz.Bytes(), 0o644); err != nil {
		log.Fatal(err)
	}
}
