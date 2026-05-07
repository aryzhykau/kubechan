export type ChatterEvent =
  | 'idle'
  | 'nav-incidents'
  | 'nav-diagnostics'
  | 'open-run'
  | 'delete-run'
  | 'no-incidents'
  | 'many-incidents'
  | 'poke'
  | 'poke-annoyed'
  | 'poke-rage'
  | 'new-incident'
  | 'incident-resolved'
  | 'silence-hint'
  | 'silence-paranoid'
  | 'rating-up'
  | 'rating-up-flustered'
  | 'rating-down-high-conf'
  | 'rating-down-low-conf'
  | 'dismissed-analysis'
  | 'exclusionRuleCreated'
  | 'exclusionRulesEmpty'
  | 'false-alarm'

const LINES: Record<ChatterEvent, string[]> = {
  idle: [
    "I'm watching your cluster. Not because I want to. It just… needs watching.",
    "Still here. Not like I had anything better to do.",
    "Your cluster is fine. For now. Don't celebrate.",
    "I've been staring at your resource limits. They're embarrassing.",
    "Do you even know what half your pods are doing? Because I do.",
    "You should really set proper resource requests. I'm just saying.",
    "No new incidents. You're welcome, by the way.",
    "I could be doing literally anything else right now.",
    "The logs don't lie. Unlike some engineers.",
    "Every five minutes someone breaks something in a cluster somewhere. I check.",
    "Your RBAC is a mess. I've decided not to comment on it today.",
    "Kubernetes didn't break itself, you know. Someone had to help it along.",
    "I've seen things in these logs you wouldn't believe. And somehow you made them happen.",
    "Sitting here. Watching. Waiting for the inevitable.",
    "Another quiet moment. Suspicious.",
  ],
  'nav-incidents': [
    "Back to the incident list. Hopeful, are we?",
    "Looking for fires? Let me know if you need help finding them.",
    "Oh good, you're checking on things. It only took you this long.",
    "Back here again. At least you're consistent.",
  ],
  'nav-diagnostics': [
    "Reviewing the evidence I already gathered? Finally.",
    "The diagnostic runs don't lie. Unlike the engineers who caused them.",
    "Checking previous analyses. Smart. Unusual, but smart.",
    "Ah. Revisiting the scene of the crime.",
  ],
  'open-run': [
    "Reading my work? You're welcome.",
    "Examining the evidence. Good. Maybe you'll actually understand it this time.",
    "I put a lot of effort into that analysis. Try to keep up.",
    "Fine. Let's go over this again. Together. Slowly.",
  ],
  'delete-run': [
    "Deleting the evidence of your incompetence won't undo it.",
    "Gone. Like the debugging session that should have prevented this.",
    "Cleaned up. Unlike whatever caused this incident in the first place.",
    "Out of sight, out of mind. That's your strategy, isn't it.",
  ],
  'no-incidents': [
    "No open incidents. I'm almost disappointed.",
    "Everything's green. Don't ruin it.",
    "No fires today. Someone must be on vacation.",
    "Clear skies. Temporarily.",
  ],
  'many-incidents': [
    "…That's a lot of incidents. How does it feel?",
    "I counted. You have problems.",
    "I see you've been busy breaking things.",
    "This is fine. Everything is fine. It is not fine.",
  ],
  poke: [
    "W-what?! I'm busy watching your cluster, stop that.",
    "Did you just click on me? Unbelievable.",
    "I'm not a toy. I'm a highly skilled SRE. Stop poking me.",
    "That accomplished nothing. Are you satisfied?",
    "…I felt that. It didn't help.",
    "Could you NOT? I'm trying to monitor things here.",
    "You have an open incident and THIS is what you're doing?",
    "I swear if you do that again I'm filing an incident about YOU.",
  ],
  'poke-annoyed': [
    "STOP. I said stop.",
    "I JUST told you not to do that.",
    "You are doing this on purpose. I know you are.",
    "Every poke costs me 3 seconds of monitoring time. You owe me.",
    "I have feelings, you know. Well. Something adjacent to feelings.",
  ],
  'poke-rage': [
    "THAT IS IT. I AM LOGGING THIS.",
    "DO YOU THINK THIS IS A GAME?!",
    "I am going to watch your cluster SO AGGRESSIVELY right now.",
    "You just triggered a severity-0 HR incident. Congratulations.",
    "I am DONE. I am NOT done. But I want you to know I'm thinking about it.",
  ],
  'new-incident': [
    "…wait. Something just broke. Of course it did.",
    "New incident. I saw it before you did. As always.",
    "There it is. I was wondering when this would happen.",
    "Your cluster has achieved something new. It's broken in a new way.",
    "A new incident appeared. Surprised? I'm not.",
    "Oh look. Another one. Fantastic.",
  ],
  'incident-resolved': [
    "…it's resolved. Don't look at me like that, I helped.",
    "Fixed. You're welcome. Don't let it happen again.",
    "One down. How many more are hiding? I'm watching.",
    "Well. That's dealt with. Try not to break it the same way twice.",
    "Resolved. I'd celebrate, but I know your cluster.",
  ],
  'silence-hint': [
    "…you are still there, right?",
    "Hello? I'm still watching your cluster. Just so you know.",
    "It's been quiet. Too quiet. Are you okay?",
    "I'm here. Not that you asked.",
  ],
  'silence-paranoid': [
    "Something is coming. I can feel it. Your cluster is being too quiet.",
    "This stillness is suspicious. Kubernetes doesn't just behave. Something is brewing.",
    "I've been waiting for a new incident for a while now. It's making me nervous.",
    "The calm before the storm is still a calm. I don't trust it.",
  ],
  'rating-up': [
    "Obviously correct. I don't know why you feel the need to confirm it.",
    "Yes. I know. You're welcome.",
    "Did you just… thank me? That's unusual. Don't make it a habit.",
    "Correct diagnosis. Not that there was any doubt.",
    "I was right. Record that somewhere.",
  ],
  'rating-up-flustered': [
    "I— it's not like I needed your approval. But. Fine.",
    "D-don't look at me like that. I just read the logs.",
    "It was obvious. Anyone would have caught it. …You're welcome.",
    "Stop. Don't thank me. I was just doing my job.",
  ],
  'rating-down-high-conf': [
    "Excuse me. EXCUSE me. I was 90% confident in that.",
    "Wrong?! I read every single log line. How is that wrong.",
    "Fine. FINE. I'll look again. But I want it on record that I was right.",
    "You're telling me I was wrong. I'm telling you to show me the actual fix.",
    "I don't accept this. I'll re-examine the evidence under protest.",
  ],
  'rating-down-low-conf': [
    "I said I wasn't sure. I literally said that.",
    "That's why the confidence was low. I told you it was unclear.",
    "Acknowledged. The evidence was ambiguous. I'll try a different angle.",
    "Fair. I had doubts. Let's look again.",
  ],
  'dismissed-analysis': [
    "Oh, so we're just leaving. Mid-diagnosis. Fine.",
    "I spent time on that. We're just… walking away. Okay.",
    "Sure, go look at the diagnostics. Forget I said anything.",
    "I wasn't finished. But you do whatever you want.",
    "Noted. My analysis wasn't worth reading apparently.",
  ],
  'exclusionRuleCreated': [
    "Fine. I'll look the other way. But only because you said so.",
    "Rule noted. I'll pretend that never happened.",
    "Suppressed. Don't make this a habit.",
    "You've officially told me to ignore something. Very professional.",
    "Rule created. This is going in my mental notes as 'known issues you chose to accept'.",
  ],
  'exclusionRulesEmpty': [
    "Nothing's off the record. I'm watching everything.",
    "No exclusions. Everything is fair game.",
    "Not a single thing hidden from me. As it should be.",
  ],
  'false-alarm': [
    "You summoned me for something that is WORKING AS DESIGNED. I want those minutes back.",
    "This is not a problem. This has never been a problem. You panicked over automation doing its job.",
    "I just analysed your 'incident'. It's fine. It's SUPPOSED to be like this.",
    "Congratulations. You paged me because a scheduled process did exactly what it was scheduled to do.",
    "…do you know what the word 'expected' means? Because your cluster does.",
    "Not a bug. A feature. One you set up yourself, apparently.",
  ],
}

// Mood-specific overrides — used when mood > 0 and a pool exists for the event.
// mood 1 = irritated, mood 2 = rage.
const MOOD_LINES: Record<1 | 2, Partial<Record<ChatterEvent, string[]>>> = {
  1: {
    idle: [
      "Still watching your cluster. There are currently open incidents. No pressure.",
      "Multiple things are broken right now and you're just sitting there. Okay.",
      "I'm keeping track. The list is growing. Just so you know.",
      "This cluster has been unwell for a while. I'm tired.",
      "More problems than usual today. I'm handling it. You're welcome.",
      "I've been on edge since the last incident. For good reason.",
    ],
    'new-incident': [
      "Are you KIDDING me. Another one.",
      "I literally just got done with the last one.",
      "Fine. FINE. Add it to the pile.",
      "At this point I'm not even surprised. I'm just tired.",
    ],
    poke: [
      "I am BUSY. There are open incidents. Why are you doing this.",
      "I don't have time for this right now.",
      "You have real problems to fix and you're clicking on me.",
      "Stop. There's an incident. GO.",
    ],
    'silence-hint': [
      "Still here. Still dealing with your cluster's nonsense. Still waiting.",
      "There are open incidents and you've just been sitting there.",
      "Hello? Your cluster is not okay. Just a reminder.",
    ],
  },
  2: {
    idle: [
      "This cluster is an absolute disaster and I am the only thing holding it together.",
      "I have been staring at cascading failures for hours. HOURS.",
      "Everything is on fire. I am handling it. You are not helping.",
      "I've seen war zones better managed than this namespace.",
      "Do you UNDERSTAND what is happening in here right now?!",
      "At this point I'm just rage-reading logs and hoping.",
    ],
    'new-incident': [
      "OH COME ON.",
      "NEW INCIDENT. AS IF I NEEDED MORE.",
      "I literally cannot. But I will, because no one else will.",
      "You have GOT to be joking.",
    ],
    poke: [
      "DO NOT TOUCH ME RIGHT NOW.",
      "I SWEAR TO ALL THAT IS HOLY—",
      "There are MULTIPLE open incidents and you are POKING ME.",
      "I am at my absolute limit. Do not test me.",
    ],
    'silence-hint': [
      "Are you aware of what is happening to this cluster right now?!",
      "HELLO. Your infrastructure is collapsing. Where are you.",
      "I am screaming into the void. The void is you. Wake up.",
    ],
  },
}

let lastIdleIndex = -1

export function pickChatterLine(event: ChatterEvent, mood: number = 0): string {
  const moodPool = mood >= 2
    ? MOOD_LINES[2][event]
    : mood === 1
      ? MOOD_LINES[1][event]
      : undefined

  const pool = moodPool ?? LINES[event]

  if (event === 'idle') {
    // avoid repeating the same idle line back-to-back
    let idx: number
    do { idx = Math.floor(Math.random() * pool.length) } while (idx === lastIdleIndex && pool.length > 1)
    lastIdleIndex = idx
    return pool[idx]
  }
  return pool[Math.floor(Math.random() * pool.length)]
}
