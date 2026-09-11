// Locate only the secondary identity-verification dialog, not the normal phone-login form.
() => {
  const visible = el => !!el && el.getBoundingClientRect().width > 0 &&
    el.getBoundingClientRect().height > 0 && getComputedStyle(el).visibility !== 'hidden';
  const matches = [];
  for (const input of document.querySelectorAll('input')) {
    if (!visible(input) || !/验证码/.test(input.placeholder || '')) continue;
    for (let el = input.parentElement; el && el !== document.body; el = el.parentElement) {
      if (/短信验证码验证/.test(el.innerText) &&
          [...el.querySelectorAll('input')].filter(visible).length === 1) {
        matches.push({dialog: el, input}); break;
      }
    }
  }
  if (matches.length !== 1) return null;
  const match = matches[0];
  const candidates = [...match.dialog.querySelectorAll('button,[role="button"],div,span,a')]
    .filter(el => visible(el) && el.innerText.trim() === '验证');
  // Prefer the leaf label: its normal click bubbles to the actual control.
  match.submit = candidates.filter(el => !candidates.some(other => other !== el && el.contains(other)));
  return match;
}
