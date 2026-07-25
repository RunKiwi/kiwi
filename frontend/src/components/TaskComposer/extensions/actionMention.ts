import { mergeAttributes } from "@tiptap/core";
import Mention from "@tiptap/extension-mention";

export const ActionMention = Mention.extend({
  name: "actionMention",

  addAttributes() {
    return {
      kind: {
        default: "test",
        parseHTML: element => element.getAttribute("data-kind"),
        renderHTML: attributes => {
          if (!attributes.kind) return {};
          return { "data-kind": attributes.kind };
        },
      },
      value: {
        default: null,
        parseHTML: element => element.getAttribute("data-value"),
        renderHTML: attributes => {
          if (!attributes.value) return {};
          return { "data-value": attributes.value };
        },
      },
      label: {
        default: null,
        parseHTML: element => element.getAttribute("data-label"),
        renderHTML: attributes => {
          if (!attributes.label) return {};
          return { "data-label": attributes.label };
        },
      },
    };
  },

  parseHTML() {
    return [
      {
        tag: 'span[data-type="actionMention"]',
      },
    ];
  },

  renderHTML({ node, HTMLAttributes }) {
    const label = node.attrs.label || node.attrs.value || "unknown";
    return [
      "span",
      mergeAttributes(
        { "data-type": this.name },
        this.options.HTMLAttributes,
        HTMLAttributes,
        {
          class: "chip mention-chip",
          style: "display: inline-flex; align-items: center; gap: 4px; padding: 0px 6px; border-radius: 9999px; background: rgba(161,161,170,0.1); border: 1px solid rgba(161,161,170,0.3); color: #A1A1AA; font-size: 0.85em; font-family: monospace; user-select: none; margin: 0 2px;",
        }
      ),
      `/${label}`,
    ];
  },
});
