/* Hack Club Auth (auth.hackclub.com) sign-in flow.
 *
 * Frontend redirects user to https://auth.hackclub.com/oauth/authorize and
 * receives an authorization code at /auth. It then POSTs that code here, we
 * exchange it for an access token, fetch the user's identity, look up their
 * Slack display name on cachet.dunkirk.sh, upsert the user, and set a session.
 */
package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"schej.it/server/db"
	"schej.it/server/logger"
	"schej.it/server/models"
	"schej.it/server/responses"
	"schej.it/server/utils"
)

const (
	hackclubTokenURL  = "https://auth.hackclub.com/oauth/token"
	hackclubMeURL     = "https://auth.hackclub.com/api/v1/me"
	cachetUserURLBase = "https://cachet.dunkirk.sh/users/"
)

type hackclubTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
	ErrorMsg    string `json:"error"`
}

type hackclubMeResponse struct {
	Identity struct {
		Id           string `json:"id"`
		FirstName    string `json:"first_name"`
		LastName     string `json:"last_name"`
		PrimaryEmail string `json:"primary_email"`
		SlackId      string `json:"slack_id"`
	} `json:"identity"`
}

type cachetUserResponse struct {
	Id          string `json:"id"`
	UserId      string `json:"userId"`
	DisplayName string `json:"displayName"`
	Pronouns    string `json:"pronouns"`
	ImageUrl    string `json:"imageUrl"`
}

// @Summary Signs the user in via Hack Club Auth
// @Description Exchanges a Hack Club OAuth authorization code for an access token,
// @Description fetches the user's identity, enriches the displayName via cachet.dunkirk.sh,
// @Description and creates or updates the user in the database.
// @Tags auth
// @Accept json
// @Produce json
// @Param payload body object{code=string,timezoneOffset=int} true "Hack Club OAuth authorization code and timezone offset"
// @Success 200 {object} models.User
// @Router /auth/hackclub [post]
func signInHackclub(c *gin.Context) {
	payload := struct {
		Code           string `json:"code" binding:"required"`
		TimezoneOffset *int   `json:"timezoneOffset" binding:"required"`
	}{}
	if err := c.BindJSON(&payload); err != nil {
		return
	}

	clientId := os.Getenv("HACKCLUB_CLIENT_ID")
	clientSecret := os.Getenv("HACKCLUB_CLIENT_SECRET")
	if clientId == "" || clientSecret == "" {
		c.JSON(http.StatusInternalServerError, responses.Error{Error: "hackclub-not-configured"})
		return
	}

	redirectURI := utils.GetOrigin(c) + "/auth"

	// 1. Exchange the authorization code for an access token.
	tokenReq, _ := json.Marshal(map[string]string{
		"client_id":     clientId,
		"client_secret": clientSecret,
		"redirect_uri":  redirectURI,
		"code":          payload.Code,
		"grant_type":    "authorization_code",
	})
	tokenHTTPResp, err := http.Post(hackclubTokenURL, "application/json", bytes.NewReader(tokenReq))
	if err != nil {
		logger.StdErr.Println("hackclub token exchange error:", err)
		c.JSON(http.StatusBadGateway, responses.Error{Error: "hackclub-token-exchange-failed"})
		return
	}
	defer tokenHTTPResp.Body.Close()
	tokenBody, _ := io.ReadAll(tokenHTTPResp.Body)
	if tokenHTTPResp.StatusCode != http.StatusOK {
		logger.StdErr.Printf("hackclub token exchange %d: %s\n", tokenHTTPResp.StatusCode, string(tokenBody))
		c.JSON(http.StatusBadRequest, responses.Error{Error: "hackclub-token-exchange-failed"})
		return
	}
	var tokenResp hackclubTokenResponse
	if err := json.Unmarshal(tokenBody, &tokenResp); err != nil || tokenResp.AccessToken == "" {
		logger.StdErr.Println("hackclub token decode error:", err, "body:", string(tokenBody))
		c.JSON(http.StatusBadGateway, responses.Error{Error: "hackclub-token-exchange-failed"})
		return
	}

	// 2. Fetch the user's identity.
	meReq, _ := http.NewRequest(http.MethodGet, hackclubMeURL, nil)
	meReq.Header.Set("Authorization", "Bearer "+tokenResp.AccessToken)
	meHTTPResp, err := http.DefaultClient.Do(meReq)
	if err != nil {
		logger.StdErr.Println("hackclub /api/v1/me error:", err)
		c.JSON(http.StatusBadGateway, responses.Error{Error: "hackclub-profile-fetch-failed"})
		return
	}
	defer meHTTPResp.Body.Close()
	meBody, _ := io.ReadAll(meHTTPResp.Body)
	if meHTTPResp.StatusCode != http.StatusOK {
		logger.StdErr.Printf("hackclub /api/v1/me %d: %s\n", meHTTPResp.StatusCode, string(meBody))
		c.JSON(http.StatusBadGateway, responses.Error{Error: "hackclub-profile-fetch-failed"})
		return
	}
	var me hackclubMeResponse
	if err := json.Unmarshal(meBody, &me); err != nil {
		logger.StdErr.Println("hackclub /api/v1/me decode error:", err, "body:", string(meBody))
		c.JSON(http.StatusBadGateway, responses.Error{Error: "hackclub-profile-fetch-failed"})
		return
	}

	slackId := strings.TrimSpace(me.Identity.SlackId)
	email := strings.ToLower(strings.TrimSpace(me.Identity.PrimaryEmail))
	if slackId == "" {
		// We require a Slack ID so we can look up the displayName on cachet.
		c.JSON(http.StatusBadRequest, responses.Error{Error: "hackclub-missing-slack-id"})
		return
	}

	// 3. Fetch displayName (and avatar) from cachet.dunkirk.sh.
	displayName, picture := fetchCachetProfile(slackId)
	if displayName == "" {
		// Fall back to the name returned by Hack Club Auth.
		displayName = strings.TrimSpace(me.Identity.FirstName + " " + me.Identity.LastName)
	}

	// 4. Upsert user (look up by Slack ID first, then by email).
	existing := db.GetUserBySlackId(slackId)
	if existing == nil && email != "" {
		existing = db.GetUserByEmail(email)
	}

	var userId primitive.ObjectID
	if existing == nil {
		userData := models.User{
			Email:          email,
			FirstName:      displayName,
			SlackId:        slackId,
			Picture:        picture,
			TimezoneOffset: *payload.TimezoneOffset,
			TokenOrigin:    models.WEB,
		}
		res, err := db.UsersCollection.InsertOne(context.Background(), userData)
		if err != nil {
			logger.StdErr.Panicln(err)
		}
		userId = res.InsertedID.(primitive.ObjectID)
	} else {
		userId = existing.Id

		update := bson.M{
			"slackId": slackId,
		}
		if email != "" {
			update["email"] = email
		}
		// Honor the user's custom name if they set one in Settings.
		if existing.HasCustomName == nil || !*existing.HasCustomName {
			update["firstName"] = displayName
			update["lastName"] = ""
		}
		if picture != "" {
			update["picture"] = picture
		}
		if _, err := db.UsersCollection.UpdateByID(
			context.Background(),
			userId,
			bson.M{"$set": update},
		); err != nil {
			logger.StdErr.Panicln(err)
		}
	}

	// 5. Set the session cookie.
	session := sessions.Default(c)
	session.Set("userId", userId.Hex())
	session.Save()

	user := db.GetUserById(userId.Hex())
	c.JSON(http.StatusOK, user)
}

// fetchCachetProfile returns (displayName, imageUrl). Both may be empty if the
// lookup fails — the caller is expected to fall back gracefully.
func fetchCachetProfile(slackId string) (string, string) {
	resp, err := http.Get(cachetUserURLBase + slackId)
	if err != nil {
		logger.StdErr.Println("cachet fetch error:", err)
		return "", ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		logger.StdErr.Printf("cachet returned %d for %s\n", resp.StatusCode, slackId)
		return "", ""
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", ""
	}
	var profile cachetUserResponse
	if err := json.Unmarshal(body, &profile); err != nil {
		logger.StdErr.Println("cachet decode error:", err, "body:", string(body))
		return "", ""
	}
	return strings.TrimSpace(profile.DisplayName), strings.TrimSpace(profile.ImageUrl)
}

