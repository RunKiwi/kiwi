import { mergeAttributes } from "@tiptap/core";
import Mention from "@tiptap/extension-mention";
import { ReactNodeViewRenderer } from "@tiptap/react";
import { TaskNodeView } from "../TaskNodeView";

export const WorkMention = Mention.extend({
  name: "workMention",

  addAttributes() {
    return {
      kind: {
        default: "task",
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
      status: {
        default: null,
        parseHTML: element => element.getAttribute("data-status"),
        renderHTML: attributes => {
          if (!attributes.status) return {};
          return { "data-status": attributes.status };
        },
      },
      mode: {
        default: "Reference",
        parseHTML: element => element.getAttribute("data-mode"),
        renderHTML: attributes => {
          if (!attributes.mode) return {};
          return { "data-mode": attributes.mode };
        },
      },
    };
  },

  parseHTML() {
    return [
      {
        tag: 'span[data-type="workMention"]',
      },
    ];
  },

  renderHTML({ HTMLAttributes }) {
    return [
      "span",
      mergeAttributes(
        { "data-type": this.name },
        this.options.HTMLAttributes,
        HTMLAttributes,
        { class: "work-mention-node" } // ReactNodeViewRenderer will replace the content
      )
    ];
  },

  addNodeView() {
    return ReactNodeViewRenderer(TaskNodeView, {
      className: "inline-flex align-middle",
    });
  },
});
