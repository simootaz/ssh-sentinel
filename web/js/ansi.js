// Turns a terminal typescript (the output of script(1)) into plain text:
// escape sequences removed, carriage returns and backspaces applied the way
// a terminal would, other control characters dropped. Pure functions, no DOM.

// OSC sequences (window titles, hyperlinks): ESC ] ... BEL, or ESC ] ... ESC \
const OSC = /\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)/g;
// CSI sequences (colours, cursor moves): ESC [ params intermediates final
const CSI = /\x1b\[[\x30-\x3f]*[\x20-\x2f]*[\x40-\x7e]/g;
// 8-bit CSI, rare but cheap to handle
const CSI8 = /\x9b[\x30-\x3f]*[\x20-\x2f]*[\x40-\x7e]/g;
// Every other escape: ESC intermediates final (charset selection, keypad modes, ...)
const ESC = /\x1b[\x20-\x2f]*[\x30-\x7e]/g;
// Control characters that carry no text. Tab and newline are kept; \r and \b
// are consumed by renderLine before this runs.
const CONTROL = /[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]/g;

/** Removes every escape sequence from text and keeps the rest as is. */
export function stripEscapes(text) {
  return text.replace(OSC, '').replace(CSI, '').replace(CSI8, '').replace(ESC, '');
}

/**
 * Applies \r and \b to one line the way a terminal does: \r moves the cursor
 * to the start of the line and later characters overwrite, \b moves it one
 * cell back without erasing. Progress bars and spinners collapse to their
 * final state instead of piling up.
 */
export function renderLine(line) {
  const cells = [];
  let cursor = 0;
  for (const ch of line) {
    if (ch === '\r') { cursor = 0; continue; }
    if (ch === '\b') { if (cursor > 0) cursor--; continue; }
    if (cursor < cells.length) cells[cursor] = ch; else cells.push(ch);
    cursor++;
  }
  return cells.join('');
}

/** Full conversion: typescript bytes as a string in, readable text out. */
export function toPlainText(text) {
  const stripped = stripEscapes(text.replace(/\r\n/g, '\n'));
  return stripped
    .split('\n')
    .map((line) => renderLine(line).replace(CONTROL, ''))
    .join('\n');
}

/**
 * Raw view: nothing removed, but control characters and escapes are shown as
 * visible tokens (e.g. ␛[0m) so the original stream can still be read.
 */
export function toVisible(text) {
  return text.replace(/[\x00-\x08\x0b-\x1f\x7f]/g, (c) => {
    if (c === '\x1b') return '␛';
    if (c === '\r') return '␍';
    if (c === '\t') return c;
    return '\\x' + c.charCodeAt(0).toString(16).padStart(2, '0');
  });
}
