import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

/**
 * Guard chong loi encoding trong file dich.
 *
 * Mot tool ghi file bang ASCII se bien "Tiep tuc" thanh "Ti?p t?c": JSON van hop le,
 * test khac van xanh, va loi chi lo ra khi nguoi dung doc man hinh. Hai dau hieu duoi
 * day bat duoc no ma khong can biet truoc chuoi nao bi hong.
 */
const readMessages = (locale) =>
  readFileSync(new URL(`../../messages/${locale}.json`, import.meta.url), 'utf8');

test('vi copy keeps its diacritics', () => {
  const messages = readMessages('vi');
  // U+FFFD la dau hieu doc file sai encoding.
  assert.doesNotMatch(messages, /\uFFFD/, 'vi.json contains a Unicode replacement character');
  // Dau hoi giua hai chu cai la dau vet cua mot ky tu co dau bi ha xuong ASCII.
  const mangled = messages
    .split('\n')
    .filter((line) => /[A-Za-z\u00C0-\u1EF9]\?[a-z\u00E0-\u1EF9]/.test(line) && !line.includes('http'));
  assert.deepEqual(mangled, [], 'vi.json has ASCII-mangled Vietnamese text');
});

test('both locales expose the same Google error vocabulary', () => {
  // Mot khoa thieu o mot ben se thanh chuoi tho tren trang dang nhap.
  const keysOf = (locale) => Object.keys(JSON.parse(readMessages(locale)).console.auth.googleError).sort();
  assert.deepEqual(keysOf('vi'), keysOf('en'));
});