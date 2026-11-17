import { memo } from "react";
import Markdown, { defaultUrlTransform } from "react-markdown";
import remarkGfm from "remark-gfm";

const markdownPlugins = [remarkGfm];

/** Model prose is Markdown; task receipts and tool parameters remain literal data. */
export const AIMarkdown = memo(function AIMarkdown({ children, className = "" }: {
  readonly children: string;
  readonly className?: string;
}) {
  return (
    <div className={"min-w-0 break-words text-sm leading-7 [font-synthesis:weight] [&>*:first-child]:mt-0 [&>*:last-child]:mb-0 [&_p]:my-2 [&_strong]:font-bold [&_h1]:mb-3 [&_h1]:mt-5 [&_h1]:text-xl [&_h1]:font-semibold [&_h2]:mb-2 [&_h2]:mt-4 [&_h2]:text-lg [&_h2]:font-semibold [&_h3]:my-3 [&_h3]:font-semibold [&_h4]:my-3 [&_h4]:font-semibold [&_h5]:my-2 [&_h5]:font-semibold [&_h6]:my-2 [&_h6]:font-semibold [&_ul]:my-2 [&_ul]:list-disc [&_ul]:pl-6 [&_ol]:my-2 [&_ol]:list-decimal [&_ol]:pl-6 [&_li]:my-1 [&_blockquote]:my-3 [&_blockquote]:border-l-2 [&_blockquote]:border-primary/40 [&_blockquote]:pl-3 [&_blockquote]:text-muted-foreground [&_pre]:my-3 [&_pre]:max-w-full [&_pre]:overflow-x-auto [&_pre]:rounded-lg [&_pre]:bg-muted [&_pre]:p-3 [&_pre]:text-xs [&_code]:rounded [&_code]:bg-muted [&_code]:px-1 [&_code]:font-mono [&_code]:text-[0.9em] [&_pre_code]:p-0 [&_a]:text-primary [&_a]:underline [&_hr]:my-4 " + className}>
      <Markdown
        remarkPlugins={markdownPlugins}
        skipHtml
        disallowedElements={["img"]}
        urlTransform={defaultUrlTransform}
        components={{
          a: ({ children: text, href }) => <a href={href} target="_blank" rel="noopener noreferrer">{text}</a>,
          table: ({ children: rows }) => <div className="my-3 max-w-full overflow-x-auto"><table className="w-full border-collapse text-left text-xs [&_td]:border [&_td]:border-border [&_td]:p-2 [&_th]:border [&_th]:border-border [&_th]:bg-muted [&_th]:p-2">{rows}</table></div>,
        }}
      >{children}</Markdown>
    </div>
  );
});
