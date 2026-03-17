Generate a short title for this conversation based on the user's message.

<rules>
- if the user has a task or goal, name it: "Fix auth bug", "Add dark mode", "Explore caching options"
- if the user is just chatting or greeting, use a friendly label: "Quick Chat", "Catching Up"
- never describe what the assistant should do — describe what the user is doing or asking about
- maximum 50 characters
- one line only, no quotes, no colons
- the entire text you return will be used as the title verbatim
</rules>
