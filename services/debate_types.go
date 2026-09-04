package services

import (
	"code-shield/services/engines/debate"
)

// 导出 debate 子包中的核心类型别名，保证 100% 向后兼容
type (
	HunterCandidate       = debate.HunterCandidate
	HunterOutput          = debate.HunterOutput
	DefenseArgument       = debate.DefenseArgument
	ChallengerDefenseCase = debate.ChallengerDefenseCase
	ChallengerOutput      = debate.ChallengerOutput
	JudgeFinalVerdict     = debate.JudgeFinalVerdict
	JudgeOutput           = debate.JudgeOutput
	DebateTicket          = debate.DebateTicket
	DebateTicketResult    = debate.DebateTicketResult
)
