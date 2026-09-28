export { KnowledgeBasesEntry } from "@/features/knowledge-bases/components/knowledge-bases-entry";

// The exports below are the knowledge base page contract consumed by the admin section.
// New cross-feature consumers must go through this entry point, never deep internal paths.
export { KnowledgeBaseDetail } from "@/features/knowledge-bases/components/knowledge-base-detail";
export { KnowledgeBasePageDialogs } from "@/features/knowledge-bases/components/knowledge-base-page-dialogs";
export {
  type KnowledgeBasesPageModel,
  useKnowledgeBasesPage,
} from "@/features/knowledge-bases/hooks/use-knowledge-bases-page";
