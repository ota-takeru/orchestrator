import type { Preview } from "@storybook/react-vite";
import "../src/styles.css";

const preview: Preview = {
  parameters: {
    a11y: {
      // Existing app-level stories start as warnings; new isolated component
      // stories should opt in to `error` once their baseline is clean.
      test: "todo"
    },
    controls: {
      matchers: {
        color: /(background|color)$/i,
        date: /Date$/i
      }
    }
  }
};

export default preview;
