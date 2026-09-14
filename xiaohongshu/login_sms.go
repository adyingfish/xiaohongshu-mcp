package xiaohongshu

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"
)

// ErrSMSNotSubmitted means no submit click was attempted; the user may retry after correcting the form.
var ErrSMSNotSubmitted = errors.New("短信验证码尚未提交")

var smsCodePattern = regexp.MustCompile(`^[0-9]{4,8}$`)

func ValidLoginSMSCode(code string) bool { return smsCodePattern.MatchString(code) }

// SubmitSMSCode submits only the user-provided code through the visible official form.
// It neither requests a new SMS nor reads any SMS inbox.
func (a *LoginAction) SubmitSMSCode(ctx context.Context, code string) error {
	if !ValidLoginSMSCode(code) {
		return fmt.Errorf("%w：须为 4 至 8 位数字", ErrSMSNotSubmitted)
	}
	script := `(code) => {
   if (location.hostname !== 'www.xiaohongshu.com') return 'wrong_page';
   const locate = (` + loginSMSLocator + `);
   const form = locate();
   if (!form || form.submit.length !== 1) return 'missing_form';
   // The submit button is normally disabled until a complete code is entered.
   if (form.input.disabled || form.input.readOnly) return 'disabled';
   if (form.input.maxLength > 0 && code.length > form.input.maxLength) return 'invalid_length';
   const setter=Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value').set;
   form.input.focus();setter.call(form.input,code);
   form.input.dispatchEvent(new Event('input',{bubbles:true}));
   form.input.dispatchEvent(new Event('change',{bubbles:true}));
   return 'filled';
 }`
	res, err := a.page.Context(ctx).Timeout(5*time.Second).Eval(script, code)
	if err != nil {
		return fmt.Errorf("%w：无法填写验证码，请检查登录状态", ErrSMSNotSubmitted)
	}
	if res.Value.String() != "filled" {
		return fmt.Errorf("%w：当前页面没有可用的短信验证表单", ErrSMSNotSubmitted)
	}
	// Wait for reactive validation to enable the button, then attempt exactly one click.
	res, err = a.page.Context(ctx).Timeout(5*time.Second).Eval(`async (code) => {
   const locate = (`+loginSMSLocator+`);
   const initial = locate();
   if (!initial || initial.submit.length !== 1) return false;
   const deadline = Date.now() + 2000;
   while (Date.now() < deadline) {
     await new Promise(resolve => setTimeout(resolve, 25));
     const form = locate();
     if (!form || form.dialog !== initial.dialog || form.input !== initial.input ||
         form.submit.length !== 1 || form.submit[0] !== initial.submit[0] ||
         form.input.value !== code || form.input.disabled || form.input.readOnly ||
         location.hostname !== 'www.xiaohongshu.com') return false;
     const button = form.submit[0];
     let disabled = false;
     for (let el=button; el; el=el.parentElement) {
       if (el.disabled || el.getAttribute('aria-disabled') === 'true' ||
           (el.hasAttribute('disabled') && el.getAttribute('disabled') !== 'false') ||
           /(?:^|[-_\s])disabled(?:$|[-_\s])/.test(el.className || '')) disabled = true;
       if (el === form.dialog) break;
     }
     if (!disabled) { button.click(); return true; }
   }
   return false;
 }`, code)
	if err != nil {
		return fmt.Errorf("短信验证码提交结果未确认，请检查登录状态，不要自动重试")
	}
	if !res.Value.Bool() {
		return fmt.Errorf("%w：验证表单已变化或暂不可提交，请检查登录状态", ErrSMSNotSubmitted)
	}
	return nil
}
