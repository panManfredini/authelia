package handlers

import (
	"fmt"
	"net/url"

	"github.com/authelia/authelia/internal/authentication"
	"github.com/authelia/authelia/internal/duo"
	"github.com/authelia/authelia/internal/middlewares"
)

// SecondFactorDuoPost handler for sending a push notification via duo api.
func SecondFactorDuoPost(duoAPI duo.API) middlewares.RequestHandler {
	return func(ctx *middlewares.AutheliaCtx) {
		var requestBody signDuoRequestBody
		err := ctx.ParseBody(&requestBody)

		if err != nil {
			handleAuthenticationUnauthorized(ctx, err, mfaValidationFailedMessage)
			return
		}

		userSession := ctx.GetSession()
		remoteIP := ctx.RemoteIP().String()

		ctx.Logger.Debugf("Starting Duo Push Auth Attempt for %s from IP %s", userSession.Username, remoteIP)

		values := url.Values{}
		// { username, ipaddr: clientIP, factor: "push", device: "auto", pushinfo: `target%20url=${targetURL}`}
		values.Set("username", userSession.Username)
		values.Set("ipaddr", remoteIP)
		values.Set("factor", "push")
		values.Set("device", "auto")

		if requestBody.TargetURL != "" {
			values.Set("pushinfo", fmt.Sprintf("target%%20url=%s", requestBody.TargetURL))
		}

		duoResponse, err := duoAPI.Call(values, ctx)
		if err != nil {
			handleAuthenticationUnauthorized(ctx, fmt.Errorf("Duo API errored: %s", err), mfaValidationFailedMessage)
			return
		}

		if duoResponse.Stat == "FAIL" {
			if duoResponse.Code == 40002 {
				ctx.Logger.Warnf("Duo Push Auth failed to process the auth request for %s from %s: %s (%s), error code %d. "+
					"This error often occurs if you've not setup the username in the Admin Dashboard.",
					userSession.Username, remoteIP, duoResponse.Message, duoResponse.MessageDetail, duoResponse.Code)
			} else {
				ctx.Logger.Warnf("Duo Push Auth failed to process the auth request for %s from %s: %s (%s), error code %d.",
					userSession.Username, remoteIP, duoResponse.Message, duoResponse.MessageDetail, duoResponse.Code)
			}
		}

		if duoResponse.Response.Result != testResultAllow {
			ctx.ReplyUnauthorized()
			return
		}

		err = ctx.Providers.SessionProvider.RegenerateSession(ctx.RequestCtx)

		if err != nil {
			handleAuthenticationUnauthorized(ctx, fmt.Errorf("Unable to regenerate session for user %s: %s", userSession.Username, err), mfaValidationFailedMessage)
			return
		}

		userSession.AuthenticationLevel = authentication.TwoFactor
		userSession.LastTwoFactorChallenge = ctx.Clock.Now().Unix()
		err = ctx.SaveSession(userSession)

		if err != nil {
			handleAuthenticationUnauthorized(ctx, fmt.Errorf("Unable to update authentication level with Duo: %s", err), mfaValidationFailedMessage)
			return
		}

		Handle2FAResponse(ctx, requestBody.TargetURL)
	}
}
