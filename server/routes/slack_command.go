/* Slack slash command `/orph-time poll <args>`
 *
 * Lets a Hack Club Slack user create an Orph-time availability poll without
 * leaving Slack. The slash-command sender MUST have an Orph-time account
 * linked to their Slack ID (i.e. they signed in once via Hack Club Auth),
 * otherwise they are sent an ephemeral sign-in link.
 *
 * Examples:
 *   /orph-time poll Mon-Fri 6-9pm
 *   /orph-time poll "Team meeting" Mon-Wed 10am-12pm
 *   /orph-time poll Sat-Sun 14:00-17:00
 */
package routes

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"schej.it/server/db"
	"schej.it/server/models"
	"schej.it/server/utils"
)

func registerSlackCommand(r *gin.RouterGroup) {
	r.POST("/command", handleSlackCommand)
}

func handleSlackCommand(c *gin.Context) {
	payload := struct {
		Command  string `form:"command" binding:"required"`
		Text     string `form:"text"`
		UserId   string `form:"user_id" binding:"required"`
		UserName string `form:"user_name"`
	}{}
	if err := c.Bind(&payload); err != nil {
		return
	}

	args := tokenizeSlackArgs(payload.Text)
	if len(args) == 0 || strings.ToLower(args[0]) == "help" {
		c.JSON(http.StatusOK, slackEphemeral(slackHelpText()))
		return
	}

	switch strings.ToLower(args[0]) {
	case "poll":
		handlePollCommand(c, payload.UserId, args[1:])
	default:
		c.JSON(http.StatusOK, slackEphemeral(
			"Unknown subcommand `"+args[0]+"`.\n\n"+slackHelpText(),
		))
	}
}

func handlePollCommand(c *gin.Context, slackUserId string, args []string) {
	// 1. Enforce login: user must already exist in Orph-time, linked by slack_id.
	user := db.GetUserBySlackId(slackUserId)
	if user == nil {
		origin := utils.GetOrigin(c)
		c.JSON(http.StatusOK, slackEphemeral(
			"You need to sign in to Orph-time before using this command.\n"+
				"Sign in once with Hack Club here → "+origin+"/sign-in",
		))
		return
	}

	// 2. Parse args: [optional "name"] <day-range> <time-range>
	parsed, err := parsePollArgs(args)
	if err != nil {
		c.JSON(http.StatusOK, slackEphemeral(
			"Couldn't parse your command: "+err.Error()+"\n\n"+slackHelpText(),
		))
		return
	}

	// 3. Build dates in the user's timezone (UTC if unknown).
	loc := tzLocationFromOffsetMinutes(user.TimezoneOffset)
	dates := upcomingWeekdayDates(parsed.weekdays, parsed.startHour, parsed.startMinute, loc)
	if len(dates) == 0 {
		c.JSON(http.StatusOK, slackEphemeral("No valid days were resolved from your day range."))
		return
	}

	durationHours := parsed.endHour - parsed.startHour
	durationMinutes := parsed.endMinute - parsed.startMinute
	totalMinutes := durationHours*60 + durationMinutes
	if totalMinutes <= 0 {
		c.JSON(http.StatusOK, slackEphemeral("End time must be after start time."))
		return
	}
	duration := float32(totalMinutes) / 60.0

	// 4. Build and insert the event.
	bsonDates := make([]primitive.DateTime, 0, len(dates))
	for _, d := range dates {
		bsonDates = append(bsonDates, primitive.NewDateTimeFromTime(d))
	}
	name := parsed.name
	if name == "" {
		name = fmt.Sprintf("%s poll", strings.Join(parsed.dayLabels, "/"))
	}

	event := models.Event{
		Name:     name,
		OwnerId:  user.Id,
		Duration: &duration,
		Dates:    bsonDates,
		Type:     models.SPECIFIC_DATES,
	}
	res, err := db.EventsCollection.InsertOne(context.Background(), event)
	if err != nil {
		c.JSON(http.StatusOK, slackEphemeral("Failed to create event: "+err.Error()))
		return
	}
	eventId := res.InsertedID.(primitive.ObjectID).Hex()
	url := utils.GetOrigin(c) + "/e/" + eventId

	// 5. Reply in-channel so the team can fill it out.
	c.JSON(http.StatusOK, gin.H{
		"response_type": "in_channel",
		"text": fmt.Sprintf(
			"📅 <@%s> created an Orph-time poll: *%s*\nFill in your availability → %s",
			slackUserId, name, url,
		),
	})
}

// ---------- parsing ----------

type parsedPoll struct {
	name        string
	weekdays    []time.Weekday // resolved days, in order
	dayLabels   []string       // human labels e.g. ["Mon","Fri"]
	startHour   int
	startMinute int
	endHour     int
	endMinute   int
}

// tokenizeSlackArgs splits a text payload, honoring "quoted phrases" as one arg.
func tokenizeSlackArgs(text string) []string {
	out := []string{}
	re := regexp.MustCompile(`"([^"]*)"|'([^']*)'|(\S+)`)
	for _, m := range re.FindAllStringSubmatch(strings.TrimSpace(text), -1) {
		for _, g := range m[1:] {
			if g != "" {
				out = append(out, g)
				break
			}
		}
	}
	return out
}

// parsePollArgs expects either `<dayRange> <timeRange>` or `<name> <dayRange> <timeRange>`.
func parsePollArgs(args []string) (*parsedPoll, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("expected at least a day range and a time range")
	}

	var name string
	if len(args) >= 3 {
		// First token is the name (was likely a quoted phrase).
		name = args[0]
		args = args[1:]
	}

	dayRange := args[0]
	timeRange := args[1]

	weekdays, labels, err := parseDayRange(dayRange)
	if err != nil {
		return nil, err
	}
	sh, sm, eh, em, err := parseTimeRange(timeRange)
	if err != nil {
		return nil, err
	}
	return &parsedPoll{
		name: name, weekdays: weekdays, dayLabels: labels,
		startHour: sh, startMinute: sm, endHour: eh, endMinute: em,
	}, nil
}

var weekdayNames = map[string]time.Weekday{
	"sun": time.Sunday, "sunday": time.Sunday,
	"mon": time.Monday, "monday": time.Monday,
	"tue": time.Tuesday, "tues": time.Tuesday, "tuesday": time.Tuesday,
	"wed": time.Wednesday, "weds": time.Wednesday, "wednesday": time.Wednesday,
	"thu": time.Thursday, "thur": time.Thursday, "thurs": time.Thursday, "thursday": time.Thursday,
	"fri": time.Friday, "friday": time.Friday,
	"sat": time.Saturday, "saturday": time.Saturday,
}
var weekdayLabels = []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}

// parseDayRange accepts "Mon", "Mon-Fri", "Mon,Wed,Fri", "Mon Wed Fri", etc.
func parseDayRange(s string) ([]time.Weekday, []string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return nil, nil, fmt.Errorf("empty day range")
	}

	// Range form: "mon-fri"
	if strings.Contains(s, "-") {
		parts := strings.SplitN(s, "-", 2)
		start, ok1 := weekdayNames[strings.TrimSpace(parts[0])]
		end, ok2 := weekdayNames[strings.TrimSpace(parts[1])]
		if !ok1 || !ok2 {
			return nil, nil, fmt.Errorf("unknown weekday in range %q", s)
		}
		out := []time.Weekday{}
		labels := []string{}
		// Walk forward, wrapping Sunday→Saturday if necessary.
		d := start
		for {
			out = append(out, d)
			labels = append(labels, weekdayLabels[d])
			if d == end {
				break
			}
			d = time.Weekday((int(d) + 1) % 7)
			if len(out) > 7 {
				break
			}
		}
		return out, labels, nil
	}

	// List form: comma or space separated
	sep := regexp.MustCompile(`[,\s]+`)
	out := []time.Weekday{}
	labels := []string{}
	for _, p := range sep.Split(s, -1) {
		if p == "" {
			continue
		}
		d, ok := weekdayNames[p]
		if !ok {
			return nil, nil, fmt.Errorf("unknown weekday %q", p)
		}
		out = append(out, d)
		labels = append(labels, weekdayLabels[d])
	}
	if len(out) == 0 {
		return nil, nil, fmt.Errorf("no days parsed from %q", s)
	}
	return out, labels, nil
}

// parseTimeRange accepts "6-9pm", "9am-12pm", "18:00-21:00", "6:30pm-9pm".
func parseTimeRange(s string) (sh, sm, eh, em int, err error) {
	s = strings.ToLower(strings.ReplaceAll(s, " ", ""))
	parts := strings.SplitN(s, "-", 2)
	if len(parts) != 2 {
		err = fmt.Errorf("time range must contain a `-` (e.g. 6-9pm)")
		return
	}
	left, right := parts[0], parts[1]

	// If the left half lacks am/pm but right has it, inherit.
	leftHasMeridiem := strings.HasSuffix(left, "am") || strings.HasSuffix(left, "pm")
	rightHasMeridiem := strings.HasSuffix(right, "am") || strings.HasSuffix(right, "pm")
	if !leftHasMeridiem && rightHasMeridiem {
		left = left + right[len(right)-2:]
	}
	sh, sm, err = parseSingleTime(left)
	if err != nil {
		return
	}
	eh, em, err = parseSingleTime(right)
	return
}

var singleTimeRe = regexp.MustCompile(`^(\d{1,2})(?::(\d{2}))?(am|pm)?$`)

func parseSingleTime(s string) (h, m int, err error) {
	mt := singleTimeRe.FindStringSubmatch(s)
	if mt == nil {
		err = fmt.Errorf("could not parse time %q", s)
		return
	}
	h, _ = strconv.Atoi(mt[1])
	if mt[2] != "" {
		m, _ = strconv.Atoi(mt[2])
	}
	switch mt[3] {
	case "am":
		if h == 12 {
			h = 0
		}
	case "pm":
		if h < 12 {
			h += 12
		}
	}
	if h < 0 || h > 23 || m < 0 || m > 59 {
		err = fmt.Errorf("invalid time %q", s)
	}
	return
}

// ---------- dates / timezone ----------

// tzLocationFromOffsetMinutes converts a JS-style timezone offset (minutes
// behind UTC, e.g. PDT = 420) to a *time.Location. UTC if 0 or unknown.
func tzLocationFromOffsetMinutes(offsetMin int) *time.Location {
	// JS getTimezoneOffset returns minutes WEST of UTC (positive for PDT).
	// time.FixedZone wants seconds EAST of UTC.
	return time.FixedZone("user", -offsetMin*60)
}

// upcomingWeekdayDates returns the next occurrence (today or future) of each
// requested weekday, with the wall-clock hour/minute set in `loc`.
func upcomingWeekdayDates(days []time.Weekday, hour, minute int, loc *time.Location) []time.Time {
	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, loc)

	out := make([]time.Time, 0, len(days))
	for _, d := range days {
		offset := (int(d) - int(now.Weekday()) + 7) % 7
		// If the requested day is today but the time has already passed, skip to next week.
		if offset == 0 && today.Before(now) {
			offset = 7
		}
		out = append(out, today.AddDate(0, 0, offset).UTC())
	}
	return out
}

// ---------- slack response helpers ----------

func slackEphemeral(text string) gin.H {
	return gin.H{"response_type": "ephemeral", "text": text}
}

func slackHelpText() string {
	return "*Usage:* `/orph-time poll [name] <days> <time-range>`\n" +
		"• Days: `Mon`, `Mon-Fri`, `Sat,Sun`\n" +
		"• Time range: `6-9pm`, `9am-12pm`, `18:00-21:00`\n" +
		"• Optional quoted name: `\"Team meeting\"`\n\n" +
		"*Examples:*\n" +
		"• `/orph-time poll Mon-Fri 6-9pm`\n" +
		"• `/orph-time poll \"Hack night\" Sat-Sun 2-5pm`"
}
