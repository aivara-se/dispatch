Bun.serve({
  unix: "/root/worktrees/dispatch-m3/test.sock",
  fetch(request) {
    console.error("hit", new URL(request.url).pathname)
    return Response.json({ flows: [{ name: "review", jobs: 2 }] })
  },
})
console.error("listening")
