-- Every key belongs to one quiz hash slot and is explicitly supplied by Go.
-- Preflight types, numeric state and serialization before any write. Redis Lua
-- is isolated, not rollback-capable: unexpected infrastructure errors fail closed.
local op = ARGV[1]
local expected = {'hash','hash','zset','hash','hash','hash','hash'}
for i = 1,7 do
  local t = redis.call('TYPE',KEYS[i]).ok
  if t ~= 'none' and t ~= expected[i] then return redis.error_reply('STORAGE_TYPE') end
end
local function integer(v, max)
  local n = tonumber(v)
  if not n or n < 0 or n > max or n ~= math.floor(n) then error('STORAGE_NUMBER') end
  return n
end
local content = ARGV[2]
local function questionCount(manifest)
  local n=0;for _ in pairs(manifest) do n=n+1 end
  if n<4 or n>8 then error('STORAGE_MANIFEST_SIZE') end
  return n
end
local function validatePlan(encoded, manifest, version)
  local plan = cjson.decode(encoded)
  local total=questionCount(manifest)
  if plan.contentVersion ~= version or #plan.questionOrder ~= total then error('STORAGE_PLAN') end
  local seen = {}
  local optionSets = 0
  for _ in pairs(plan.optionOrders) do optionSets = optionSets+1 end
  if optionSets ~= total then error('STORAGE_PLAN') end
  for _,qid in ipairs(plan.questionOrder) do
    if seen[qid] or not manifest[qid] then error('STORAGE_PLAN') end
    seen[qid] = true
    local order = plan.optionOrders[qid]
    if #order ~= #manifest[qid] then error('STORAGE_PLAN_OPTIONS') end
    local allowed = {}
    for _,id in ipairs(manifest[qid]) do allowed[id] = true end
    for _,id in ipairs(order) do
      if not allowed[id] then error('STORAGE_PLAN_OPTIONS') end
      allowed[id] = nil
    end
  end
  return plan
end
if op == 'reset' then
  -- Called only by the offline development CLI; replaces precisely these seven keys.
  local cap = integer(ARGV[4],200)
  if cap < 1 then return redis.error_reply('CAPACITY') end
  local manifest = cjson.decode(ARGV[5])
  redis.call('DEL',unpack(KEYS))
  redis.call('HSET',KEYS[1],'epoch',ARGV[3],'version',0,'content',content,'capacity',cap,'manifest',ARGV[5])
  return {'ok',ARGV[3],'0'}
end
if redis.call('EXISTS',KEYS[1]) == 0 then
  if op ~= 'seed' then return {'QUIZ_NOT_FOUND'} end
  for i=2,7 do if redis.call('EXISTS',KEYS[i]) ~= 0 then return redis.error_reply('ORPHAN_STATE') end end
  local cap = integer(ARGV[4],200)
  if cap < 1 then return redis.error_reply('CAPACITY') end
  local manifest = cjson.decode(ARGV[5])
  redis.call('HSET',KEYS[1],'epoch',ARGV[3],'version',0,'content',content,'capacity',cap,'manifest',ARGV[5])
  return {'ok',ARGV[3],'0'}
end
local storedContent = redis.call('HGET',KEYS[1],'content')
if storedContent ~= content then
  if op ~= 'seed' or ARGV[6] ~= storedContent then return redis.error_reply('CONTENT_MISMATCH') end
  -- Explicit upgrade from the preceding policy. Preflight and serialize every
  -- saved plan before writing. Never replace orders, scores, receipts or epoch.
  local priorEpoch = redis.call('HGET',KEYS[1],'epoch')
  if not priorEpoch or #priorEpoch ~= 32 then return redis.error_reply('STORAGE_EPOCH') end
  integer(redis.call('HGET',KEYS[1],'version'),1000000)
  local priorCap = integer(redis.call('HGET',KEYS[1],'capacity'),200)
  if priorCap < 1 then return redis.error_reply('CAPACITY') end
  local manifestRaw = redis.call('HGET',KEYS[1],'manifest')
  if manifestRaw ~= ARGV[5] then return redis.error_reply('STORAGE_UPGRADE_MANIFEST') end
  local priorManifest = cjson.decode(manifestRaw)
  local plans = redis.call('HGETALL',KEYS[6])
  local size = redis.call('HLEN',KEYS[2])
  if size > priorCap or size ~= #plans/2 or size ~= redis.call('ZCARD',KEYS[3]) or redis.call('EXISTS',KEYS[7]) ~= 0 then return redis.error_reply('STORAGE_UPGRADE') end
  for i=1,#plans,2 do
    local plan = validatePlan(plans[i+1], priorManifest, storedContent)
    local memberRaw = redis.call('HGET',KEYS[2],plans[i])
    if not memberRaw then return redis.error_reply('STORAGE_UPGRADE_MEMBER') end
    local member = cjson.decode(memberRaw)
    if type(member.name) ~= 'string' or member.name == '' then return redis.error_reply('STORAGE_NAME') end
    local count = integer(member.count,8)
    local cursor = integer(member.cursor,8)
    local score = integer(redis.call('ZSCORE',KEYS[3],plans[i]),800)
    if score % 100 ~= 0 or score > count*100 then return redis.error_reply('STORAGE_SCORE') end
    local actualCount, actualCursor = 0,8
    for j,qid in ipairs(plan.questionOrder) do
      if redis.call('HEXISTS',KEYS[4],plans[i]..':'..qid) == 1 then actualCount = actualCount+1
      elseif actualCursor == 8 then actualCursor = j-1 end
    end
    if count ~= actualCount or cursor ~= actualCursor then return redis.error_reply('STORAGE_PROGRESS') end
    plan.contentVersion = content
    plans[i+1] = cjson.encode(plan)
  end
  for i=1,#plans,2 do redis.call('HSET',KEYS[6],plans[i],plans[i+1]) end
  redis.call('HSET',KEYS[1],'content',content,'previousContent',storedContent)
end
local epoch = redis.call('HGET',KEYS[1],'epoch')
if not epoch or #epoch ~= 32 then return redis.error_reply('STORAGE_EPOCH') end
local version = integer(redis.call('HGET',KEYS[1],'version'),1000000)
local cap = integer(redis.call('HGET',KEYS[1],'capacity'),200)
local size = redis.call('HLEN',KEYS[2])
if size ~= redis.call('ZCARD',KEYS[3]) or size ~= redis.call('HLEN',KEYS[6]) or size > cap then return redis.error_reply('STORAGE_MEMBERS') end
if op == 'seed' then return {'ok',epoch,tostring(version)} end
if op == 'preview' then return {'ok'} end
local manifest = cjson.decode(redis.call('HGET',KEYS[1],'manifest'))
local totalQuestions=questionCount(manifest)
local pid = ARGV[3]
local raw = redis.call('HGET',KEYS[2],pid)
local member = nil
local score = nil
local planRaw = redis.call('HGET',KEYS[6],pid)
local plan = nil
if raw then
  member = cjson.decode(raw)
  if type(member.name) ~= 'string' then return redis.error_reply('STORAGE_NAME') end
  member.count = integer(member.count,totalQuestions)
  member.cursor = integer(member.cursor,totalQuestions)
  if not planRaw then return redis.error_reply('STORAGE_PLAN') end
  plan = validatePlan(planRaw, manifest, content)
  local answered,cursor = 0,totalQuestions
  for i,qid in ipairs(plan.questionOrder) do
    if redis.call('HEXISTS',KEYS[4],pid..':'..qid) == 1 then answered = answered+1
    elseif cursor == totalQuestions then cursor = i-1 end
  end
  if answered ~= member.count or cursor ~= member.cursor then return redis.error_reply('STORAGE_PROGRESS') end
  score = integer(redis.call('ZSCORE',KEYS[3],pid),totalQuestions*100)
  if score % 50 ~= 0 or score > member.count*100 then return redis.error_reply('STORAGE_SCORE') end
elseif redis.call('ZSCORE',KEYS[3],pid) or planRaw then return redis.error_reply('STORAGE_MEMBER') end
if op == 'join' then
  if raw then return {'replayed',epoch,tostring(version)} end
  if size >= cap then return {'QUIZ_FULL'} end
  -- The first call asks whether a plan is needed. Resumes never invoke the RNG.
  if ARGV[5] == '' then return {'PLAN_REQUIRED'} end
  validatePlan(ARGV[5], manifest, content)
  local encoded = cjson.encode({name=ARGV[4],count=0,cursor=0})
  local nextVersion = version+1
  redis.call('HSET',KEYS[2],pid,encoded)
  redis.call('HSET',KEYS[6],pid,ARGV[5])
  redis.call('ZADD',KEYS[3],0,pid)
  redis.call('HSET',KEYS[1],'version',nextVersion)
  return {'accepted',epoch,tostring(nextVersion)}
end
local function nowMillis()
  local time = redis.call('TIME')
  return tonumber(time[1])*1000+math.floor(tonumber(time[2])/1000)
end
if op == 'start' then
  if ARGV[4] ~= epoch then return {'EPOCH_MISMATCH'} end
  if not raw then return {'NOT_JOINED'} end
  local qid = ARGV[5]
  if not plan.optionOrders[qid] then return {'QUESTION_NOT_FOUND'} end
  local field = pid..':'..qid
  local deadline = redis.call('HGET',KEYS[7],field)
  local now = nowMillis()
  if deadline then
    deadline = integer(deadline,9007199254740991)
    if deadline == 0 then return redis.error_reply('STORAGE_TIMER') end
  else
    if redis.call('HEXISTS',KEYS[4],field) == 1 then return {'QUESTION_ALREADY_ANSWERED'} end
    local duration = integer(ARGV[6],120000)
    if duration == 0 then return redis.error_reply('TIMER_DURATION') end
    deadline = now+duration
    redis.call('HSET',KEYS[7],field,deadline)
  end
  return {'ok',epoch,qid,tostring(deadline),tostring(now)}
end
if op == 'answer' then
  -- Epoch precedes membership so an old intent cannot cross a reset boundary.
  if ARGV[4] ~= epoch then return {'EPOCH_MISMATCH'} end
  if not raw then return {'NOT_JOINED'} end
  local rid,qid,option = ARGV[5],ARGV[6],ARGV[7]
  local requestField = pid..':'..rid
  local answerField = pid..':'..qid
  local previous = redis.call('HGET',KEYS[5],requestField)
  if previous then
    local receipt = cjson.decode(previous)
    if receipt.questionId ~= qid or receipt.selectedOptionId ~= option then return {'IDEMPOTENCY_CONFLICT'} end
    return {'replayed',previous}
  end
  previous = redis.call('HGET',KEYS[4],answerField)
  if previous then return {'ALREADY_ANSWERED',previous} end
  if member.count >= totalQuestions then return redis.error_reply('STORAGE_PROGRESS') end
  local points = integer(ARGV[8],100)
  if points ~= 0 and points ~= 100 then return redis.error_reply('POINTS') end
  local correct = points == 100
  local deadline = redis.call('HGET',KEYS[7],answerField)
  if deadline then
    deadline = integer(deadline,9007199254740991)
    if deadline == 0 then return redis.error_reply('STORAGE_TIMER') end
  end
  local now = nowMillis()
  -- Missing a start command cannot bypass the full-credit deadline.
  local timedOut = not deadline or now >= deadline
  if correct and timedOut then points = 50 end
  local total = score+points
  local nextVersion = version+1
  local question = cjson.decode(ARGV[12])
  local options = {}
  for _,oid in ipairs(plan.optionOrders[qid]) do
    local found = nil
    for _,o in ipairs(question.options) do if o.id == oid then found = o end end
    if not found then return redis.error_reply('STORAGE_PLAN_OPTIONS') end
    table.insert(options,found)
  end
  question.options = options
  local position,cursor = 0,totalQuestions
  for i,id in ipairs(plan.questionOrder) do
    if id == qid then position = i
    elseif cursor == totalQuestions and redis.call('HEXISTS',KEYS[4],pid..':'..id) == 0 then cursor = i-1 end
  end
  if position == 0 then return redis.error_reply('STORAGE_PLAN') end
  local receipt = cjson.encode({submissionId=rid,quizId=ARGV[9],epoch=epoch,questionId=qid,
    selectedOptionId=option,correctness=correct,pointsAwarded=points,timedOut=timedOut,deadlineMs=deadline or 0,acceptedAtMs=now,
    totalScoreAtAcceptance=total,acceptedVersion=nextVersion,correctOptionId=ARGV[10],explanation=ARGV[11],question=question,questionNumber=position})
  member.count = member.count+1
  member.cursor = cursor
  local encodedMember = cjson.encode(member)
  redis.call('HSET',KEYS[4],answerField,receipt)
  redis.call('HSET',KEYS[5],requestField,receipt)
  redis.call('ZADD',KEYS[3],total,pid)
  redis.call('HSET',KEYS[2],pid,encodedMember)
  redis.call('HSET',KEYS[1],'version',nextVersion)
  return {'accepted',receipt}
end
if op == 'snapshot' then
  if pid ~= '' and not raw then return {'NOT_JOINED'} end
  local rows = redis.call('ZREVRANGE',KEYS[3],0,-1,'WITHSCORES')
  local participants = redis.call('HGETALL',KEYS[2])
  local receipts = {}
  if pid ~= '' then
    for _,qid in ipairs(plan.questionOrder) do
      local receipt = redis.call('HGET',KEYS[4],pid..':'..qid)
      if receipt then table.insert(receipts,receipt) end
    end
    if #receipts ~= member.count then return redis.error_reply('STORAGE_RECEIPTS') end
  end
  return {'ok',epoch,tostring(version),rows,participants,receipts,planRaw or ''}
end
return redis.error_reply('UNKNOWN_OPERATION')
