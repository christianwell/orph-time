import Vue from "vue"
import VueRouter from "vue-router"
import Landing from "@/views/Landing"
import { get } from "@/utils"

Vue.use(VueRouter)

const routes = [
  {
    path: "/",
    name: "landing",
    component: Landing,
  },
  {
    path: "/home",
    name: "home",
    component: () => import("@/views/Home.vue"),
    props: true,
  },
  {
    path: "/settings",
    name: "settings",
    component: () => import("@/views/Settings.vue"),
  },
  {
    path: "/e/:eventId",
    name: "event",
    component: () => import("@/views/Event.vue"),
    props: true,
  },
  {
    path: "/e/:eventId/responded",
    name: "responded",
    component: () => import("@/views/Responded.vue"),
    props: true,
  },
  {
    path: "/g/:groupId",
    name: "group",
    component: () => import("@/views/Group.vue"),
    props: true,
  },
  {
    path: "/s/:signUpId",
    name: "signUp",
    component: () => import("@/views/SignUp.vue"),
    props: true,
  },
  {
    path: "/sign-in",
    name: "sign-in",
    component: () => import("@/views/SignIn.vue"),
  },
  {
    path: "/sign-up",
    name: "sign-up",
    component: () => import("@/views/SignIn.vue"),
    props: { initialIsSignUp: true },
  },
  {
    path: "/auth",
    name: "auth",
    component: () => import("@/views/Auth.vue"),
  },
  {
    path: "/privacy-policy",
    name: "privacy-policy",
    component: () => import("@/views/PrivacyPolicy.vue"),
  },
  {
    path: "/cookie-settings",
    name: "cookie-settings",
    component: () => import("@/components/CookieSettings.vue"),
  },
  {
    path: "/stripe-redirect",
    name: "stripe-redirect",
    component: () => import("@/views/StripeRedirect.vue"),
  },
  {
    path: "/test",
    name: "test",
    component: () => import("@/views/Test.vue"),
  },
  {
    path: "*",
    name: "404",
    component: () => import("@/views/PageNotFound.vue"),
  },
]

const router = new VueRouter({
  mode: "history",
  base: process.env.BASE_URL,
  routes,
})

// Routes that do NOT require authentication. Everything else does.
const publicRoutes = [
  "sign-in",
  "sign-up",
  "auth",
  "privacy-policy",
  "cookie-settings",
  "404",
]

router.beforeEach(async (to, from, next) => {
  try {
    await get("/auth/status")

    // Signed-in users shouldn't see sign-in / sign-up / landing — send to home.
    if (["sign-in", "sign-up", "landing"].includes(to.name)) {
      next({ name: "home" })
    } else {
      next()
    }
  } catch (err) {
    // Not signed in — only public routes are accessible.
    if (publicRoutes.includes(to.name)) {
      next()
    } else {
      next({ name: "sign-in" })
    }
  }
})

export default router
