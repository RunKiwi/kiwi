import { mergeAttributes } from "@tiptap/core";
import Mention from "@tiptap/extension-mention";

export const RefMention = Mention.extend({
  name: "refMention",

  addAttributes() {
    return {
      kind: {
        default: "file",
        parseHTML: element => element.getAttribute("data-kind"),
        renderHTML: attributes => {
          if (!attributes.kind) {
            return {};
          }
          return { "data-kind": attributes.kind };
        },
      },
      value: {
        default: null,
        parseHTML: element => element.getAttribute("data-value"),
        renderHTML: attributes => {
          if (!attributes.value) {
            return {};
          }
          return { "data-value": attributes.value };
        },
      },
      label: {
        default: null,
        parseHTML: element => element.getAttribute("data-label"),
        renderHTML: attributes => {
          if (!attributes.label) {
            return {};
          }
          return { "data-label": attributes.label };
        },
      },
    };
  },

  parseHTML() {
    return [
      {
        tag: 'span[data-type="refMention"]',
      },
    ];
  },

  renderHTML({ node, HTMLAttributes }) {
    const value = node.attrs.value || node.attrs.label || "unknown";
    return [
      "span",
      mergeAttributes(
        { "data-type": this.name },
        this.options.HTMLAttributes,
        HTMLAttributes,
        {
          class: "chip mention-chip",
          style: "display: inline-flex; align-items: center; gap: 4px; padding: 0px 6px; border-radius: 9999px; background: rgba(147,198,69,0.1); border: 1px solid rgba(147,198,69,0.3); color: #93C645; font-size: 0.85em; font-family: monospace; user-select: none; margin: 0 2px;",
        }
      ),
      `@${value}`,
    ];
  },
});
