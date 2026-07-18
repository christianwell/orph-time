/* Slack DM-invite feature.
 *
 * Lets a signed-in user invite Hack Club Slack members to fill out an event's
 * availability poll by sending each of them a direct message from the Slack
 * bot. Requires SLACK_BOT_TOKEN (a bot user OAuth token starting with `xoxb-`)
 * with `chat:write` and `im:write` scopes.
 */
package routes

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"schej.it/server/db"
	"schej.it/server/logger"
	"schej.it/server/middleware"
	"schej.it/server/responses"
	"schej.it/server/utils"
)

func InitSlack(router *gin.RouterGroup) {
	slackRouter := router.Group("/slack")

	slackRouter.POST("/invite", middleware.AuthRequired(), sendSlackInvites)
	registerSlackCommand(slackRouter)
}

type slackInvitePayload struct {
	EventId  string   `json:"eventId" binding:"required"`
	SlackIds []string `json:"slackIds" binding:"required"`
	Message  string   `json:"message"` // optional custom prefix
}

type slackInviteResult struct {
	SlackId string `json:"slackId"`
	Ok      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
}

// @Summary Sends Slack DM invites for an event
// @Description Sends a direct message via the Hack Club Slack bot to each
// @Description specified Slack user ID with a link to the event.
// @Tags slack
// @Accept json
// @Produce json
// @Param payload body slackInvitePayload true "Event ID, list of Slack user IDs (e.g. U0123ABC), and optional message"
// @Success 200 {array} slackInviteResult
// @Router /slack/invite [post]
func sendSlackInvites(c *gin.Context) {
	var payload slackInvitePayload
	if err := c.BindJSON(&payload); err != nil {
		return
	}

	botToken := os.Getenv("SLACK_BOT_TOKEN")
	if botToken == "" {
		c.JSON(http.StatusInternalServerError, responses.Error{Error: "slack-bot-not-configured"})
		return
	}

	// Look up the event (accepts either an ObjectID or a short ID).
	event := db.GetEventByEitherId(payload.EventId)
	if event == nil {
		c.JSON(http.StatusNotFound, responses.Error{Error: "event-not-found"})
		return
	}

	// Who's sending the invite (used in the message body).
	session := sessions.Default(c)
	sender := db.GetUserById(session.Get("userId").(string))
	senderName := "Someone"
	if sender != nil {
		senderName = strings.TrimSpace(sender.FirstName + " " + sender.LastName)
		if senderName == "" {
			senderName = "Someone"
		}
	}

	origin := utils.GetOrigin(c)
	eventUrl := fmt.Sprintf("%s/e/%s", origin, payload.EventId)
	eventName := event.Name
	if eventName == "" {
		eventName = "an event"
	}

	results := make([]slackInviteResult, 0, len(payload.SlackIds))
	for _, raw := range payload.SlackIds {
		slackId := normalizeSlackId(raw)
		if slackId == "" {
			results = append(results, slackInviteResult{SlackId: raw, Ok: false, Error: "invalid slack id"})
			continue
		}

		channel, err := slackOpenIm(botToken, slackId)
		if err != nil {
			results = append(results, slackInviteResult{SlackId: slackId, Ok: false, Error: err.Error()})
			continue
		}

		text := buildInviteMessage(senderName, eventName, eventUrl, payload.Message)
		if err := slackPostMessage(botToken, channel, text); err != nil {
			results = append(results, slackInviteResult{SlackId: slackId, Ok: false, Error: err.Error()})
			continue
		}

		results = append(results, slackInviteResult{SlackId: slackId, Ok: true})
	}

	c.JSON(http.StatusOK, results)
}

// normalizeSlackId accepts forms like "U0123ABC", "<@U0123ABC>", "@U0123ABC"
// and returns the bare Slack user ID, or "" if it doesn't look valid.
func normalizeSlackId(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "<@")
	s = strings.TrimSuffix(s, ">")
	s = strings.TrimPrefix(s, "@")
	s = strings.ToUpper(s)
	if len(s) < 2 || (s[0] != 'U' && s[0] != 'W') {
		return ""
	}
	return s
}

func buildInviteMessage(sender, eventName, eventUrl, custom string) string {
	custom = strings.TrimSpace(custom)
	if custom == "" {
		custom = fmt.Sprintf("%s would like to know when you're available for *%s*.", sender, eventName)
	}
	return fmt.Sprintf("%s\n\nFill in your availability here → %s", custom, eventUrl)
}

// --- Slack Web API helpers ---

type slackOpenImResp struct {
	Ok      bool   `json:"ok"`
	Error   string `json:"error"`
	Channel struct {
		Id string `json:"id"`
	} `json:"channel"`
}

func slackOpenIm(token, userId string) (string, error) {
	body, _ := json.Marshal(map[string]string{"users": userId})
	req, _ := http.NewRequest("POST", "https://slack.com/api/conversations.open", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out slackOpenImResp
	if err := json.Unmarshal(raw, &out); err != nil {
		logger.StdErr.Println("slack conversations.open decode:", err, string(raw))
		return "", fmt.Errorf("decode failed")
	}
	if !out.Ok {
		return "", fmt.Errorf(out.Error)
	}
	return out.Channel.Id, nil
}

type slackPostMessageResp struct {
	Ok    bool   `json:"ok"`
	Error string `json:"error"`
}

func slackPostMessage(token, channel, text string) error {
	body, _ := json.Marshal(map[string]any{
		"channel":   channel,
		"text":      text,
		"unfurl_links": false,
	})
	req, _ := http.NewRequest("POST", "https://slack.com/api/chat.postMessage", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out slackPostMessageResp
	if err := json.Unmarshal(raw, &out); err != nil {
		logger.StdErr.Println("slack chat.postMessage decode:", err, string(raw))
		return fmt.Errorf("decode failed")
	}
	if !out.Ok {
		return fmt.Errorf(out.Error)
	}
	return nil
}
