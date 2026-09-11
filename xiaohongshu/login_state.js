() => {
  const visible = el => !!el && el.getBoundingClientRect().width > 0 &&
    el.getBoundingClientRect().height > 0 && getComputedStyle(el).visibility !== 'hidden';
  const images = [...document.querySelectorAll('img.qrcode-img')].filter(visible);
  // The verification dialog and initial login dialog both use .qrcode-img.
  // Select the image inside its own identity-verification dialog, never the first image.
  let verification = null;
  for (const img of images) {
    for (let el = img.parentElement; el && el !== document.body; el = el.parentElement) {
      if (/扫码验证身份/.test(el.innerText) && /请通过验证|保护账号安全/.test(el.innerText) &&
          el.querySelectorAll('img.qrcode-img').length === 1) {
        verification = {el, img}; break;
      }
    }
    if (verification) break;
  }
  const login = [...document.querySelectorAll('.login-container')].find(visible);
  const verificationVisible = verification || [...document.querySelectorAll('div,p,span')].some(
    el => visible(el) && el.children.length === 0 && /扫码验证身份/.test(el.innerText));
  if (verificationVisible) {
    const expired = !!verification && /已过期|点击.*刷新/.test(verification.el.innerText);
    return JSON.stringify({status: expired ? 'verification_expired' : 'verification_required',
      img: expired ? '' : verification?.img.src || '',
      message: expired ? '身份验证二维码已过期，请调用 get_login_qrcode 在原会话刷新。' :
        '第一次扫码已确认，小红书要求二次身份验证。请使用已登录该账号的小红书 App 扫描这张新的验证二维码（网页提示约 1 分钟有效），不要重扫第一次的登录二维码。'});
  }
  if (!login && document.querySelector('.main-container .user .link-wrapper .channel')) {
    return JSON.stringify({status: 'logged_in'});
  }
  const text = login?.innerText || '';
  if (/二维码已过期/.test(text)) return JSON.stringify({status: 'qr_expired', message: '登录二维码已过期，请调用 get_login_qrcode 在原会话刷新。'});
  const scanned = /扫码成功|请在手机上确认/.test(text);
  return JSON.stringify({status: scanned ? 'awaiting_confirmation' : 'awaiting_scan',
    img: scanned ? '' : images.find(img => login?.contains(img))?.src || '',
    message: scanned ? '网页已收到扫码，请在手机确认后继续调用 check_login_status；如出现二次验证，将返回新的验证二维码。' :
      '请扫描当前登录二维码，然后调用 check_login_status 检查进度；等待期间重复获取会复用本次会话。'});
}
