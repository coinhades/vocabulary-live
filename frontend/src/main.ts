import { createApp } from "vue";
import "./style.css";
import "./theme-light.css";
import "./learning.css";
import "./surface-polish.css";
async function mount() {
  const { default: App } = await (location.pathname === "/instructor"
    ? import("./InstructorApp.vue")
    : import("./App.vue"));
  createApp(App).mount("#app");
}

void mount().catch(() => {
  const message = document.getElementById("boot-message");
  if (message)
    message.textContent =
      "Vocabulary Live could not load. Check your connection and try again.";
  const retry = document.getElementById("boot-retry");
  if (retry) {
    retry.hidden = false;
    retry.addEventListener("click", () => location.reload());
  }
});
