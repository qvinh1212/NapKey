package mail

import (
	"fmt"
	"net/url"
	"strings"
)

// VerificationMessage builds the email confirming a new address.
//
// The link carries the token in the query string. That is the only practical way
// to deliver it by email, and it is why the token is single-use and short-lived:
// a URL ends up in browser history, in referrer headers, and sometimes in a
// corporate mail scanner's logs.
func VerificationMessage(baseURL, locale, email, token string) Message {
	link := fmt.Sprintf("%s/%s/verify-email?token=%s", strings.TrimRight(baseURL, "/"),
		localeOrDefault(locale), url.QueryEscape(token))

	if localeOrDefault(locale) == "en" {
		return Message{
			To:      email,
			Subject: "Confirm your NapKey email address",
			TextBody: "Welcome to NapKey.\n\n" +
				"Confirm your email address to finish setting up your account:\n\n" +
				link + "\n\n" +
				"This link works once and expires in 24 hours.\n\n" +
				"If you did not create a NapKey account, ignore this message.\n",
		}
	}
	return Message{
		To:      email,
		Subject: "Xác nhận địa chỉ email NapKey của bạn",
		TextBody: "Chào bạn,\n\n" +
			"Xác nhận địa chỉ email để hoàn tất việc tạo tài khoản NapKey:\n\n" +
			link + "\n\n" +
			"Liên kết chỉ dùng được một lần và hết hạn sau 24 giờ.\n\n" +
			"Nếu bạn không tạo tài khoản NapKey, hãy bỏ qua email này.\n",
	}
}

// PasswordResetMessage builds the password reset email.
func PasswordResetMessage(baseURL, locale, email, token string) Message {
	link := fmt.Sprintf("%s/%s/reset-password?token=%s", strings.TrimRight(baseURL, "/"),
		localeOrDefault(locale), url.QueryEscape(token))

	if localeOrDefault(locale) == "en" {
		return Message{
			To:      email,
			Subject: "Reset your NapKey password",
			TextBody: "Someone asked to reset the password for this NapKey account.\n\n" +
				"If it was you, set a new password here:\n\n" +
				link + "\n\n" +
				"This link works once and expires in 1 hour.\n\n" +
				"If it was not you, ignore this message. Your password stays unchanged.\n",
		}
	}
	return Message{
		To:      email,
		Subject: "Đặt lại mật khẩu NapKey",
		TextBody: "Có yêu cầu đặt lại mật khẩu cho tài khoản NapKey này.\n\n" +
			"Nếu là bạn, đặt mật khẩu mới tại đây:\n\n" +
			link + "\n\n" +
			"Liên kết chỉ dùng được một lần và hết hạn sau 1 giờ.\n\n" +
			"Nếu không phải bạn, hãy bỏ qua email này. Mật khẩu của bạn không thay đổi.\n",
	}
}

// localeOrDefault falls back to Vietnamese, the default locale per DESIGN.md
// section 8.
func localeOrDefault(locale string) string {
	if strings.EqualFold(locale, "en") {
		return "en"
	}
	return "vi"
}
