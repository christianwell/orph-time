<template>
  <div
    v-if="type === 'timeful'"
    class="tw-flex tw-items-center tw-select-none tw-cursor-pointer"
  >
    <span class="tw-text-xl sm:tw-text-2xl tw-font-bold tw-text-green">
      Orph-time
    </span>
    
  </div>
  <v-img
    v-else
    :alt="alt"
    class="shrink tw-cursor-pointer"
    contain
    :src="src"
    transition="fade-transition"
    :width="width"
  />
</template>

<script>
import { isPhone } from "@/utils"

export default {
  name: "Logo",

  props: {
    type: {
      type: "timeful" | "betterwhen2meet" | "aprilfools",
      default: "timeful",
    },
  },

  computed: {
    isPhone() {
      return isPhone(this.$vuetify)
    },
    alt() {
      if (this.type === "betterwhen2meet") {
        return "Betterwhen2meet Logo"
      }

      return "Timeful Logo"
    },
    src() {
      switch (this.type) {
        case "betterwhen2meet":
          return require("@/assets/april_fools_logo.png")
        case "aprilfools":
          return require("@/assets/april_fools_logo.png")
      }
    },
    width() {
      switch (this.type) {
        case "betterwhen2meet":
          return this.isPhone ? 200 : 300
        case "aprilfools":
          return this.isPhone ? 200 : 300
      }
    },
  },
}
</script>
