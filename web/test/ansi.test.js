// node --test web/test   (no dependency, Node 20 or newer)
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { stripEscapes, renderLine, toPlainText, toVisible } from '../js/ansi.js';

test('stripEscapes removes colours, cursor moves, OSC titles and charset selection', () => {
  assert.equal(stripEscapes('\x1b[1;32mok\x1b[0m'), 'ok');
  assert.equal(stripEscapes('\x1b[2K\x1b[1Gprompt$ '), 'prompt$ ');
  assert.equal(stripEscapes('\x1b]0;user@host: ~\x07$ ls'), '$ ls');
  assert.equal(stripEscapes('\x1b]8;;https://example.com\x1b\\link\x1b]8;;\x1b\\'), 'link');
  assert.equal(stripEscapes('\x1b(B\x1b[?2004htext\x1b[?2004l'), 'text');
  assert.equal(stripEscapes('\x9b31mred'), 'red');
});

test('renderLine applies carriage returns and backspaces like a terminal', () => {
  assert.equal(renderLine('progress 10%\rprogress 50%\rprogress 100%'), 'progress 100%');
  assert.equal(renderLine('abc\rX'), 'Xbc');
  assert.equal(renderLine('abc\b\bZ'), 'aZc');
  assert.equal(renderLine('\b\bok'), 'ok');
  assert.equal(renderLine('plain'), 'plain');
});

test('toPlainText handles a script(1) typescript end to end', () => {
  const ts = 'Script started on 2026-09-15 10:30:01+00:00\r\n' +
    '\x1b]0;deploy@web-01: ~\x07\x1b[01;32mdeploy@web-01\x1b[00m:\x1b[01;34m~\x1b[00m$ ls\r\n' +
    'bin  \x1b[01;34msrc\x1b[0m\r\n' +
    'downloading \r10%\r100%\r\n' +
    'Script done on 2026-09-15 10:30:09+00:00\r\n';
  assert.equal(toPlainText(ts),
    'Script started on 2026-09-15 10:30:01+00:00\n' +
    'deploy@web-01:~$ ls\n' +
    'bin  src\n' +
    '100%loading \n' +
    'Script done on 2026-09-15 10:30:09+00:00\n');
});

test('toPlainText drops other control characters but keeps tabs and newlines', () => {
  assert.equal(toPlainText('a\tb\x07\x00c\nd'), 'a\tbc\nd');
});

test('toVisible shows escapes and control characters as tokens', () => {
  assert.equal(toVisible('\x1b[0m\r\n\x07'), '␛[0m␍\n\\x07');
  assert.equal(toVisible('tab\there'), 'tab\there');
});
