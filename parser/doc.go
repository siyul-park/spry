// Package parser is a hand-written recursive-descent parser that follows the
// Python 3.14 grammar (Grammar/python.gram) and recovers from syntax errors.
//
// Input: source text or a token stream. Output: an ast tree plus diagnostics.
// Allowed spry imports: token, scanner and ast. It is part of the frontend layer.
package parser
