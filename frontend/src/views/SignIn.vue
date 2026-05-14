<template>
  <div
    class="tw-flex tw-min-h-screen tw-items-center tw-justify-center tw-bg-light-gray tw-px-4"
  >
    <div class="tw-w-full tw-max-w-[420px]">
      <!-- Logo -->
      <div class="tw-mb-8 tw-flex tw-justify-center">
        <router-link :to="{ name: 'landing' }">
          <Logo type="timeful" />
        </router-link>
      </div>

      <v-card class="tw-rounded-xl tw-px-2 tw-py-6">
        <v-card-title class="tw-flex tw-flex-col tw-items-center tw-pb-0">
          <div class="tw-text-2xl tw-font-medium">Welcome to Orph-time</div>
          <div class="tw-mt-1 tw-text-sm tw-font-normal tw-text-dark-gray">
            Sign in with your Hack Club account
          </div>
        </v-card-title>
        <v-card-text class="tw-flex tw-flex-col tw-items-center tw-pt-6">
          <v-btn
            block
            color="primary"
            class="tw-mb-3"
            :loading="redirecting"
            :disabled="redirecting || !clientId"
            @click="signInWithHackClub"
          >
            Login with Hack Club
          </v-btn>
          <div
            v-if="!clientId"
            class="tw-mb-3 tw-text-center tw-text-xs tw-text-error"
          >
            Hack Club client ID is not configured
            (<code>VUE_APP_HACKCLUB_CLIENT_ID</code>).
          </div>
          <div class="tw-text-center tw-text-xs">
            By continuing, you agree to our
            <router-link
              class="tw-text-blue"
              :to="{ name: 'privacy-policy' }"
            >
              privacy policy
            </router-link>
          </div>
        </v-card-text>
      </v-card>
    </div>
  </div>
</template>

<script>
import { authTypes } from "@/constants"
import Logo from "@/components/Logo.vue"

export default {
  name: "SignIn",

  metaInfo() {
    return {
      title: "Sign In - Orph-time",
    }
  },

  components: {
    Logo,
  },

  data() {
    return {
      redirecting: false,
      clientId: process.env.VUE_APP_HACKCLUB_CLIENT_ID || "",
    }
  },

  methods: {
    signInWithHackClub() {
      if (!this.clientId || this.redirecting) return
      this.redirecting = true

      const state = encodeURIComponent(
        JSON.stringify({ type: authTypes.HACKCLUB })
      )
      const redirectUri = `${window.location.origin}/auth`
      const scope = "email name slack_id"

      const url =
        "https://auth.hackclub.com/oauth/authorize" +
        `?client_id=${encodeURIComponent(this.clientId)}` +
        `&redirect_uri=${encodeURIComponent(redirectUri)}` +
        `&response_type=code` +
        `&scope=${encodeURIComponent(scope)}` +
        `&state=${state}`

      window.location.href = url
    },
  },
}
</script>
