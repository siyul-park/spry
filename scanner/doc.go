// Package scanner turns Python 3.14 source text into tokens, including
// INDENT/DEDENT, implicit line joining, and PEP 701 f-string and PEP 750
// t-string tokens.
//
// Input: source bytes. Output: a token stream with positions.
// Allowed spry imports: token only. It is part of the frontend layer.
package scanner
