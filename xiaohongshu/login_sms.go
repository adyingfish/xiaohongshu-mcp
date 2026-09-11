package xiaohongshu

import (
	"context"
	"fmt"
	"regexp"
	"time"
)

var smsCodePattern = regexp.MustCompile(`^[0-9]{4,8}$`)

func ValidLoginSMSCode(code string) bool { return smsCodePattern.MatchString(code) }

// SubmitSMSCode submits only the user-provided code through the visible official form.
// It neither requests a new SMS nor reads any SMS inbox.
func (a *LoginAction) SubmitSMSCode(ctx context.Context, code string) error {
	if !ValidLoginSMSCode(code) {
		return fmt.Errorf("短信验证码须为 4 至 8 位数字")
	}
	script := `(code) => {
   if (location.hostname !== 'www.xiaohongshu.com') return 'wrong_page';
   const locate = (` + loginSMSLocator + `);
   const form = locate();
   if (!form || form.submit.length !== 1) return 'missing_form';
   const button=form.submit[0];
   if (form.input.disabled || button.closest('[disabled],[aria-disabled="true"]')) return 'disabled';
   const setter=Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value').set;
   form.input.focus();setter.call(form.input,code);
   form.input.dispatchEvent(new Event('input',{bubbles:true}));
   form.input.dispatchEvent(new Event('change',{bubbles:true}));
   return 'filled';
 }`
	res, err := a.page.Context(ctx).Timeout(5*time.Second).Eval(script, code)
	if err != nil {
		return fmt.Errorf("无法填写短信验证码，请检查登录状态")
	}
	if res.Value.String() != "filled" {
		return fmt.Errorf("当前页面没有可用的短信验证表单")
	}
	// Let the framework update button state, then revalidate the same form and value before one click.
	res, err = a.page.Context(ctx).Timeout(5*time.Second).Eval(`async (code) => {
   await new Promise(resolve => setTimeout(resolve, 50));
   const form = (`+loginSMSLocator+`)();
   if (!form || form.submit.length !== 1 || form.input.value !== code || location.hostname!=='www.xiaohongshu.com') return false;
   const button=form.submit[0];
   for (let el=button;el && el!==form.dialog;el=el.parentElement) {
     if (el.disabled || el.getAttribute('aria-disabled')==='true' || /(?:^|[-_\s])disabled(?:$|[-_\s])/.test(el.className || '')) return false;
   }
   button.click(); return true;
 }`, code)
	if err != nil {
		return fmt.Errorf("短信验证码提交结果未确认，请检查登录状态，不要自动重试")
	}
	if !res.Value.Bool() {
		return fmt.Errorf("短信验证表单已变化或暂不可提交，请检查登录状态")
	}
	return nil
}
