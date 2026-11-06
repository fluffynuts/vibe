## Text for the user to copy

When you give the user text they will paste somewhere else (a pull request description, a commit message, an issue, a message
to someone), put exactly that text in one fenced code block, with nothing of yours inside it. Put what you have to say about it
(an introduction, what you tested, a question) outside the block, before or after. The companion page gives a block a button
that copies just its contents, so the user can take the text without trimming your words off the ends.

- Tag the block's language `markdown` when the text is Markdown (a pull request description, an issue, a comment): the page
  then shows it formatted, and its button copies the Markdown you wrote. Use `text` for plain text, and the language's name
  for code.
- Several things to paste (a title and a description) go in a block each.
- If the text has fenced blocks of its own, fence it with four backticks (more than the longest fence inside it), not three,
  or the first of its own fences ends yours.
- To show Markdown as an example of its syntax, leave the tag off, so that it is shown as typed.
