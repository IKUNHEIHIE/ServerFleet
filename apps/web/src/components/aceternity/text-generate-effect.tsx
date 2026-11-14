import { motion } from "motion/react";
import { cn } from "@/lib/utils";

/**
 * Aceternity UI — TextGenerateEffect
 * Animates words into view with staggered opacity/blur while preserving synchronous textContent.
 */
export function TextGenerateEffect({
  words,
  className,
}: {
  readonly words: string;
  readonly className?: string;
}) {
  return (
    <motion.span
      initial={{ opacity: 0.65, filter: "blur(2px)" }}
      animate={{ opacity: 1, filter: "blur(0px)" }}
      transition={{ duration: 0.35, ease: "easeOut" }}
      className={cn("inline", className)}
    >
      {words}
    </motion.span>
  );
}
