<template>
  <v-dialog v-model="show" max-width="520" persistent>
    <v-card class="tw-rounded-xl tw-p-2">
      <v-card-title class="tw-flex tw-items-center tw-gap-2">
        <v-icon color="primary">mdi-slack</v-icon>
        <span class="tw-text-lg tw-font-medium">Invite via Slack DM</span>
      </v-card-title>

      <v-card-text>
        <div class="tw-mb-2 tw-text-sm tw-text-very-dark-gray">
          Paste one or more Hack Club Slack user IDs (e.g.
          <code>U0123ABC</code>), separated by commas, spaces, or new lines.
          You can also paste <code>&lt;@U0123ABC&gt;</code> mentions copied
          from Slack.
        </div>
        <v-textarea
          v-model="slackIdsRaw"
          label="Slack user IDs"
          placeholder="U0123ABC, U0456DEF&#10;@U0789GHI"
          outlined
          rows="3"
          autofocus
          hide-details
          class="tw-mb-3"
        />
        <v-textarea
          v-model="customMessage"
          label="Custom message (optional)"
          :placeholder="defaultMessagePreview"
          outlined
          rows="2"
          hide-details
          class="tw-mb-1"
        />
        <div class="tw-mb-3 tw-text-xs tw-text-dark-gray">
          The event link will be appended automatically.
        </div>

        <!-- Results -->
        <div v-if="results.length > 0" class="tw-mt-3">
          <div class="tw-mb-1 tw-text-sm tw-font-medium">Results</div>
          <ul class="tw-text-sm">
            <li
              v-for="r in results"
              :key="r.slackId"
              :class="r.ok ? 'tw-text-green' : 'tw-text-error'"
            >
              <v-icon :color="r.ok ? 'success' : 'error'" small>
                {{ r.ok ? "mdi-check-circle" : "mdi-alert-circle" }}
              </v-icon>
              <code class="tw-mx-1">{{ r.slackId }}</code>
              <span v-if="!r.ok">— {{ r.error }}</span>
            </li>
          </ul>
        </div>

        <div v-if="error" class="tw-mt-3 tw-text-sm tw-text-error">
          {{ error }}
        </div>
      </v-card-text>

      <v-card-actions class="tw-px-4 tw-pb-4">
        <v-spacer />
        <v-btn text @click="close" :disabled="sending">Close</v-btn>
        <v-btn
          color="primary"
          :loading="sending"
          :disabled="parsedIds.length === 0"
          @click="send"
        >
          Send {{ parsedIds.length > 0 ? `(${parsedIds.length})` : "" }}
        </v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>

<script>
import { post } from "@/utils"

export default {
  name: "SlackInviteDialog",

  props: {
    value: { type: Boolean, default: false },
    eventId: { type: String, required: true },
    eventName: { type: String, default: "" },
  },

  data() {
    return {
      slackIdsRaw: "",
      customMessage: "",
      sending: false,
      results: [],
      error: "",
    }
  },

  computed: {
    show: {
      get() {
        return this.value
      },
      set(v) {
        this.$emit("input", v)
      },
    },
    parsedIds() {
      return Array.from(
        new Set(
          this.slackIdsRaw
            .split(/[\s,]+/)
            .map((s) => s.trim())
            .filter(Boolean)
        )
      )
    },
    defaultMessagePreview() {
      const name = this.eventName || "an event"
      return `Hey! Could you fill out your availability for ${name}?`
    },
  },

  watch: {
    value(v) {
      if (v) {
        this.results = []
        this.error = ""
      }
    },
  },

  methods: {
    close() {
      if (this.sending) return
      this.show = false
    },
    async send() {
      this.sending = true
      this.error = ""
      this.results = []
      try {
        const res = await post("/slack/invite", {
          eventId: this.eventId,
          slackIds: this.parsedIds,
          message: this.customMessage,
        })
        this.results = res || []
      } catch (e) {
        this.error =
          e?.parsed?.error === "slack-bot-not-configured"
            ? "Slack bot is not configured on the server (SLACK_BOT_TOKEN missing)."
            : e?.message || "Failed to send invites"
      } finally {
        this.sending = false
      }
    },
  },
}
</script>
