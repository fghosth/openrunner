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
