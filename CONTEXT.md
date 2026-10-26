# Openrunner

自托管的个人运动训练分析应用：导入/同步活动数据，计算训练负荷与生理模型（源自公开运动科学论文），帮助单个运动员评估体能状态与疲劳度。

## Language

**Athlete（运动员档案）**:
一个运动员的完整数据域：活动、区间配置、赛季、度量结果都挂在 Athlete 下。单用户安装可拥有多个 Athlete 档案（如"我-公路"、"我-越野"）。
_Avoid_: User, account, rider（GC 术语）, cyclist

**Activity（活动）**:
一次运动训练的记录，是所有度量计算的原子单元。运动类型（骑行/跑步/游泳…）是 Activity 的属性而非独立概念。
_Avoid_: Ride（GC 术语，骑行偏义）, workout, session

**Stream（逐秒序列）**:
Activity 内按固定采样间隔记录的连续数据通道（功率/心率/速度/海拔/距离等）。训练负荷指标（xPower、W'bal、TRIMP 等）的输入；云同步只给平均值时该 Activity 无 Stream。
_Avoid_: samples, raw data, trace

**Critical Speed（CS，临界速度）**:
跑步版的临界功率：理论上可长时间维持的最高配速。与 D' 配对构成跑步的速度-时长模型。
_Avoid_: 临界配速阈值（不统一）, running CP

**D'（D-prime）**:
超临界速度下可消耗的有限运动量（距离当量），是 CS 之上的"电池"。类比骑行模型的 W'。其实时余额称 D'balance（D'bal）。
_Avoid_: 无氧跑步容量（旧称）

**Estimated Metric（估算指标）**:
Activity 无 Stream 时，用日均值近似计算并显式标注的度量（如 avgHR→TRIMP、avg pace→rTSS）。有 Stream 时不产生估算指标。
_Avoid_: 近似值, fake data

**RaceResult（比赛成绩）**:
一条可作为「表现观测点」的成绩记录（日期 + 距离 + 用时），不要求是官方赛事 ——
自己跑的 5K/10K 全力计时同样有效，而且是拟合 Banister 模型最需要的样本。
_Avoid_: PR, personal record（那指的是最快分段，见 PersonalBest）

**PersonalBest（个人最佳）**:
某个目标距离（5K/10K/半马/全马）的**活动内部最快分段**，不是「距离接近该距离的整条活动」。
口径与 Garmin / Strava 的 PR 一致。
_Avoid_: 最快活动, best activity

**Banister Model（刺激-响应模型）**:
把训练负荷分解为「变强」（正效应，衰减慢）与「疲劳」（负效应，衰减快）两部分，
用比赛成绩作为观测点反推参数，从而给出当前表现与未来预测。
本项目固定时间常数 τ₁=45 天 / τ₂=15 天，因此对 p₀/k₁/k₂ 是线性最小二乘。
_Avoid_: 成绩预测模型（太泛）, fitness model

**Riegel Projection（距离外推）**:
把某个距离的成绩按幂律 `t₂ = t₁ · (d₂/d₁)^α` 换算到另一距离。α 优先用本人多距离成绩
做 log-log 回归（个人化，业余跑者通常 1.08–1.15），数据不足时退回 Riegel 教科书值 1.06。
_Avoid_: 换算, 等价成绩

**AI Prompt（AI 分析提示词）**:
把档案、训练负荷、强度分布、活动明细、个人最佳与训练目标整理成的一段结构化 Markdown。
本应用**不调用任何外部 AI**，只生成提示词，由用户自行复制到对话式 AI。
_Avoid_: AI 报告（应用不生成结论，只准备材料）
